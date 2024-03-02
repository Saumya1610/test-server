package main

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"github.com/rs/cors"
)

var ctx = context.Background()
var client *redis.Client

var CHARACTERS = []string{
	"cat",
	"defuse",
	"shuffle",
	"exploding",
}

func generateRandomCards() []string {
	rand.Seed(time.Now().UnixNano())
	randomDeck := make([]string, 0)

	for i := 0; i < 5; i++ {
		index := rand.Intn(len(CHARACTERS))
		randomDeck = append(randomDeck, CHARACTERS[index])
	}

	return randomDeck
}

func main() {
	// Initialize Redis client
	client = redis.NewClient(&redis.Options{
		Addr:     "redis-11333.c330.asia-south1-1.gce.cloud.redislabs.com:11333",
		Password: "K5buAV402UxwpMmEeTmgmw7oDmGqoE0j",
	})
	defer func() {
		if err := client.Close(); err != nil {
			fmt.Println("Error closing Redis client:", err)
		}
	}()

	// Create a new Gin router
	router := gin.Default()

	// Enable CORS using the rs/cors package
	router.Use(corsMiddleware())

	// Define a handler for the root endpoint
	router.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, "Hello, this is a Gin backend server with CORS support!")
	})

	// Define a handler for storing username in Redis
	router.POST("/store-username", storeUsernameHandler)

	// Define a handler for retrieving all stored usernames
	router.GET("/get-all-usernames", getAllUsernamesHandler)

	// New endpoint to update player stats and reflect in the leaderboard
	router.POST("/updatePlayerStats/:id", updatePlayerStatsHandler)

	// Random cards deck
	router.GET("/get-random-cards", func(c *gin.Context) {
		randomCards := generateRandomCards()
		c.JSON(http.StatusOK, gin.H{"cards": randomCards})
	})

	// Start the server on port 8080
	fmt.Println("Server is listening on port 8080...")
	err := router.Run(":8080")
	if err != nil {
		fmt.Println("Error:", err)
	}
}

// Function to set up CORS middleware using github.com/rs/cors
func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Create a new CORS handler with default options
		corsHandler := cors.Default()

		// Allow all origins, headers, and methods
		corsHandler.HandlerFunc(c.Writer, c.Request)

		// Continue processing the request
		c.Next()
	}
}

// Handler for storing username in Redis
func storeUsernameHandler(c *gin.Context) {
	// Retrieve username from the request body
	var requestBody struct {
		Player string `json:"player" binding:"required"`
	}

	if err := c.ShouldBindJSON(&requestBody); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Check if the player name already exists
	existingID, err := client.Get(ctx, "player:byname:"+requestBody.Player).Result()
	if err != nil && err != redis.Nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check player name existence"})
		return
	}

	if existingID != "" {
		// Player name already exists, return the existing ID
		c.JSON(http.StatusOK, gin.H{"message": "Player name already exists", "id": existingID})
		return
	}

	// Generate a unique UUID
	id := uuid.New().String()

	// Store username in Redis hash with associated ID, win counter, loss counter, and timestamp
	err = client.HMSet(ctx, "player:"+id, map[string]interface{}{
		"id":      id,
		"player":  requestBody.Player,
		"wins":    0,
		"losses":  0,
		"total":   0,
		"created": time.Now().Format(time.RFC3339),
	}).Err()

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to store player in Redis"})
		return
	}

	// Set a marker for the existence of the player name
	err = client.Set(ctx, "player:byname:"+requestBody.Player, id, 0).Err()
	if err != nil {
		fmt.Println("Warning: Failed to set marker for player name:", err)
	}

	c.JSON(http.StatusOK, gin.H{"message": "Player stored successfully", "id": id})
}

// Handler for retrieving all stored usernames with stats
func getAllUsernamesHandler(c *gin.Context) {
	// Retrieve all usernames from Redis hashes
	userKeys, err := client.Keys(ctx, "player:*").Result()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve players from Redis"})
		return
	}

	// Get the details for each username
	var userStats []gin.H
	for _, key := range userKeys {
		// Check if the key is a hash
		keyType, err := client.Type(ctx, key).Result()
		if err != nil {
			fmt.Printf("Error checking key type for key %s: %v\n", key, err)
			continue
		}

		if keyType != "hash" {
			fmt.Printf("Skipping non-hash key: %s\n", key)
			continue
		}

		userDetails, err := client.HGetAll(ctx, key).Result()
		if err != nil {
			fmt.Printf("Error retrieving details for key %s: %v\n", key, err)
			continue
		}

		// Convert map[string]string to map[string]interface{}
		userDetailsInterface := make(map[string]interface{})
		for k, v := range userDetails {
			userDetailsInterface[k] = v
		}

		// Calculate the total (sum of wins and losses)
		wins, _ := strconv.Atoi(userDetails["wins"])
		losses, _ := strconv.Atoi(userDetails["losses"])
		total := wins + losses
		userDetailsInterface["total"] = total

		userStats = append(userStats, userDetailsInterface)
	}

	fmt.Printf("Retrieved PlayerStats: %+v\n", userStats)

	c.JSON(http.StatusOK, gin.H{"players": userStats})
}

// Handler for updating player stats
func updatePlayerStatsHandler(c *gin.Context) {
	// Retrieve player ID and game result from the request body
	var requestBody struct {
		Win  int `json:"win"`
		Loss int `json:"loss"`
	}

	if err := c.ShouldBindJSON(&requestBody); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get the player ID from the URL parameters
	playerID := c.Param("id")

	// Get the current player stats
	key := "player:" + playerID
	playerStats, err := client.HGetAll(ctx, key).Result()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve player stats from Redis"})
		return
	}

	// Update the win and loss fields based on the game result
	wins, _ := strconv.Atoi(playerStats["wins"])
	losses, _ := strconv.Atoi(playerStats["losses"])

	wins += requestBody.Win
	losses += requestBody.Loss

	// Update the player stats in Redis
	err = client.HMSet(ctx, key, map[string]interface{}{
		"wins":   strconv.Itoa(wins),
		"losses": strconv.Itoa(losses),
	}).Err()

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update player stats in Redis"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Player stats updated successfully"})
};