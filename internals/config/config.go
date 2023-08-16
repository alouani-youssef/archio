// Copyright (c) 2023-202X ArchIO.

package config

import (
	"io/ioutil"
	"log"
	"os"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v2"
)

type EnvVars struct {
	DatabaseURL string `yaml:"DATABASE_URL"`
	APIKey      string `yaml:"API_KEY"`
}

func configLoader() {
	// Load .env file
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	// Load env.yaml file
	yamlFile, err := ioutil.ReadFile("../../env.yaml")
	if err != nil {
		log.Fatal("Error reading env.yaml file")
	}

	var envVars EnvVars
	err = yaml.Unmarshal(yamlFile, &envVars)
	if err != nil {
		log.Fatal("Error parsing env.yaml")
	}

	// Set environment variables
	os.Setenv("DATABASE_URL", envVars.DatabaseURL)
	os.Setenv("API_KEY", envVars.APIKey)

	// Now you can access environment variables using os.Getenv
	dbURL := os.Getenv("DATABASE_URL")
	apiKey := os.Getenv("API_KEY")

	// Use dbURL and apiKey in your application
	log.Println("Database URL:", dbURL)
	log.Println("API Key:", apiKey)
}
