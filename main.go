package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"time"

	"github.com/chzyer/readline"
)

// const ollamaUrl = "http://bulbasaur.bearded-piano.ts.net:11434"

const configFileName = "config.json"
const defaultOllamaUrl = "http://localhost:11434"
const logo = ` ┓┓ ┓       
┏┫┃ ┃┏┓┏┳┓┏┓
┗┻┗┛┗┗┻┛┗┗┗┻
`
const versionNumber = "v0.0.5"

// A program that lets your talk to ollama from the command line and formats the responses nicely and streams the responses
// back to the user in real time.
func main() {
	fmt.Printf("%s", logo)
	fmt.Printf("\033[90m%s\033[0m\n\n", versionNumber)

	var ollamaUrl string
	var model string
	var config bool
	var listModels bool

	flag.StringVar(&ollamaUrl, "url", "", "The url of the ollama server. Most likely this is something like http://localhost:11434 or http://myserver:11434.")
	flag.StringVar(&model, "model", "", "The model to use for the chat")
	flag.BoolVar(&config, "config", false, "Enters configuration mode. This will prompt you for the url and the default model to use. This will be saved to a config file.")
	flag.BoolVar(&listModels, "list-models", false, "List all available models")

	// Parse the flags
	flag.Parse()

	// Setup configuration file to store default values for the url and the model
	if config {
		var config DLLamaConfig

		// // Prompt the user for their url and default model
		var url string
		var defaultModel string

		fmt.Printf("Enter the url of the ollama server: (%s)\n\n", defaultOllamaUrl)
		fmt.Scanln(&url)

		if url == "" {
			url = defaultOllamaUrl
		}

		fmt.Printf("\nCalling %s to get available models...\n", url)

		defaultModel, err := promptForModelSelection(url)

		if err != nil {
			if err == ErrOllamaServerConnection {
				fmt.Printf("\nCould not connect to server. Please ensure the server is running and the url is correct.\n\n")
				return
			} else if err == ErrInvalidURL {
				fmt.Printf("\nAttempted url is invalid. Please try again.\n\n")
				return
			}

			fmt.Printf("\nError fetching models from ollama server: %v\n\n", err)
			return
		}

		config.Url = url
		config.DefaultModel = defaultModel

		err = saveToConfigFile(&config)

		if err != nil {
			fmt.Printf("Error saving config: %v\n", err)
			return
		}

		fmt.Printf("\nConfiguration saved.\n\n")
		return
	}

	// Make sure the url and the model are not empty
	if ollamaUrl == "" || model == "" {

		// if they are not provided, try to get them from the config file
		config, err := readFromConfigFile()

		if err != nil {
			if err == ErrConfigFileNotFound {
				fmt.Printf("Necessary values not provided. Please provide the url and model using the command line flags.\n\ndllama -url http://localhost:11434/ -model llama3.2\n\nAlternatively, default values can be set using the -config option.\n\nFor more information use the -help flag.\n\n")
				return
			}

			fmt.Printf("Error reading config file: %v\n", err)
			return
		}

		if config != nil {
			if ollamaUrl == "" {
				ollamaUrl = config.Url
			}

			if model == "" {
				model = config.DefaultModel
			}
		}
	}

	if listModels {
		models, err := fetchAvailableModels(ollamaUrl)

		if err != nil {
			if err == ErrOllamaServerConnection {
				fmt.Printf("Could not connect to server. Please ensure the server is running and the url is correct.\n")
				return
			}

			fmt.Printf("Error fetching models from ollama server: %v\n", err)
			return
		}

		fmt.Printf("Available models:\n\n")

		for _, m := range models {
			fmt.Println(m)
		}

		fmt.Println()

		return
	}

	rl, err := readline.New("\033[93m>> \033[0m")

	if err != nil {
		fmt.Printf("Error initializing readline: %v\n", err)
		return
	}

	defer rl.Close()

	var conversationHistory []ChatMessage

	for {
		fmt.Printf("\033[96m### Enter your prompt (or type 'exit' to quit) ###\033[0m \n\n")

		// fmt.Printf("\033[92m[%s]: \033[0m", userTimeStamp.Local().Format("2006-01-02 15:04:05"))

		prompt, err := rl.Readline()

		if err != nil {
			fmt.Printf("\nError reading input: %v", err)
			return
		}

		if prompt == "exit" {
			break
		}

		conversationHistory = append(conversationHistory, ChatMessage{
			Role:         "user",
			Content:      prompt,
			TimeStampUTC: time.Now().UTC(),
		})

		fmt.Println()

		httpClient := http.Client{
			Timeout: time.Second * 300,
		}

		// Create chat request with history
		chatRequest := ChatRequest{
			Model:    model,
			Prompt:   prompt,
			Messages: conversationHistory,
		}

		reqBody, err := json.Marshal(chatRequest)

		if err != nil {
			fmt.Printf("Error marshalling request: %v\n", err)
			return
		}

		chatPath, err := url.JoinPath(ollamaUrl, "/api/chat")

		if err != nil {
			fmt.Printf("Error joining url path: %v\n", err)
			return
		}

		resp, err := httpClient.Post(chatPath, "application/json", bytes.NewBuffer(reqBody))

		if err != nil {
			fmt.Printf("Error sending request: %v\n", err)
			return
		}

		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)

		var assistantResponse string
		var assistantTimeStamp time.Time = time.Now().UTC()

		fmt.Printf("\033[93m[%s]: \033[0m", assistantTimeStamp.Local().Format("2006-01-02 15:04:05"))

		for scanner.Scan() {
			var streamResp StreamResponse

			err = json.Unmarshal(scanner.Bytes(), &streamResp)

			if err != nil {
				fmt.Printf("Error decoding stream: %v\n", err)
				continue
			}

			assistantResponse += streamResp.Message.Content
			fmt.Print(streamResp.Message.Content)

			if streamResp.Done {
				fmt.Println()
				break
			}
		}

		if err := scanner.Err(); err != nil {
			fmt.Printf("Error reading stream: %v\n", err)
		}

		conversationHistory = append(conversationHistory, ChatMessage{
			Role:         "assistant",
			Content:      assistantResponse,
			TimeStampUTC: assistantTimeStamp,
		})

		fmt.Println()
	}

	fmt.Println()
}

func fetchAvailableModels(ollamaUrl string) ([]string, error) {
	httpClient := http.Client{
		Timeout: time.Second * 10,
	}

	fetchModelsPath, err := url.JoinPath(ollamaUrl, "/api/tags")

	if err != nil {
		fmt.Printf("Error joining url path: %v\n", err)
		return nil, err
	}

	resp, err := httpClient.Get(fetchModelsPath)

	if err != nil {
		var dnsError *net.DNSError
		if errors.As(err, &dnsError) {
			return nil, ErrOllamaServerConnection
		}

		var procError *url.Error

		if errors.As(err, &procError) {
			return nil, ErrInvalidURL
		}

		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			return nil, ErrOllamaServerConnection
		} else {
			return nil, fmt.Errorf("error getting models: %v", resp.Status)
		}
	}

	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)

	var response ListModelsResponse

	for scanner.Scan() {
		err = json.Unmarshal(scanner.Bytes(), &response)

		if err != nil {
			fmt.Printf("Error decoding response: %v\n", err)
			return nil, err
		}
	}

	var models []string

	for _, m := range response.Models {
		models = append(models, m.Name)
	}

	return models, err
}

func saveToConfigFile(config *DLLamaConfig) error {
	configPath, err := getUserConfigPath()

	if err != nil {
		return fmt.Errorf("error getting user config path: %w", err)
	}

	configDir := filepath.Dir(configPath)

	err = os.MkdirAll(configDir, 0755)

	if err != nil {
		return fmt.Errorf("error creating config directory: %w", err)
	}

	configFile, err := os.Create(configPath)

	if err != nil {
		return fmt.Errorf("error creating config file: %w", err)
	}

	defer configFile.Close()

	err = json.NewEncoder(configFile).Encode(config)

	if err != nil {
		return fmt.Errorf("error encoding config file: %w", err)
	}

	return nil
}

func readFromConfigFile() (*DLLamaConfig, error) {
	configPath, err := getUserConfigPath()

	if err != nil {
		return nil, fmt.Errorf("error getting user config path: %w", err)
	}

	configFile, err := os.Open(configPath)

	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrConfigFileNotFound
		}

		return nil, fmt.Errorf("error opening config file: %w", err)
	}

	defer configFile.Close()

	var config DLLamaConfig

	err = json.NewDecoder(configFile).Decode(&config)

	if err != nil {
		return nil, fmt.Errorf("error decoding config file: %w", err)
	}

	return &config, nil
}

func promptForModelSelection(url string) (string, error) {
	models, err := fetchAvailableModels(url)

	if err != nil {
		return "", err
	}

	fmt.Printf("\nAvailable models:\n\n")

	for i, m := range models {
		fmt.Printf("%d. %s\n", i+1, m)
	}

	fmt.Println()

	var selection int

	for {
		fmt.Printf("Enter the number of the model you would like to use: \n\n")

		_, err := fmt.Scanln(&selection)

		if err != nil {
			fmt.Printf("Error reading input: %v\n", err)
			continue
		}

		if selection < 1 || selection > len(models) {
			fmt.Println("Invalid selection. Please try again.")
			continue
		}

		break
	}

	return models[selection-1], nil
}

type ChatRequest struct {
	Model    string        `json:"model"`
	Prompt   string        `json:"prompt"`
	Messages []ChatMessage `json:"messages"`
}

type ChatMessage struct {
	Role         string    `json:"role"`
	Content      string    `json:"content"`
	TimeStampUTC time.Time `json:"timestamp_utc"`
}

type StreamResponse struct {
	Model     string      `json:"model"`
	CreatedAt string      `json:"created_at"`
	Message   ChatMessage `json:"message"`
	Done      bool        `json:"done"`
}

type DLLamaConfig struct {
	Url          string `json:"url"`
	DefaultModel string `json:"default_model"`
}

type ListModelsResponse struct {
	Models []Model `json:"models"`
}

func getUserConfigPath() (string, error) {
	if runtime.GOOS == "windows" {
		appData := os.Getenv("APPDATA")
		if appData == "" {
			return "", fmt.Errorf("APPDATA environment variable is not set")
		}
		return filepath.Join(appData, "dllama", configFileName), nil
	} else {
		usr, err := user.Current()
		if err != nil {
			return "", fmt.Errorf("error getting current user: %w", err)
		}
		return filepath.Join(usr.HomeDir, ".config", "dllama", configFileName), nil
	}
}

type Model struct {
	Name    string       `json:"name"`
	Details ModelDetails `json:"details"`
}

type ModelDetails struct {
	Format            string `json:"format"`
	Family            string `json:"family"`
	ParameterSize     string `json:"parameter_size"`
	QuantizationLevel string `json:"quantization_level"`
}

var ErrConfigFileNotFound = fmt.Errorf("config file does not exist")
var ErrOllamaServerConnection = fmt.Errorf("could not connect to server")
var ErrInvalidURL = fmt.Errorf("invalid url")
