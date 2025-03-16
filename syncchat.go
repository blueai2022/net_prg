package api

import (
	"fmt"
	"log"
	"maps"
	"slices"
	"sync"

	"github.com/blueai2022/mc/rating"
)

// syncAllToDecisions synchronizes all follower chats to reach a decision state.
func (server *Server) syncAllToDecisions(clientRequest ChatRequest, chatServerAddr string, backendURLs map[string]string) ([]*rating.Rating, error) {
	// Get all follower chat IDs
	followerChatIds, err := server.chatState.followerChatIds(clientRequest.ChatID, slices.Collect(maps.Keys(backendURLs)))
	if err != nil {
		return nil, fmt.Errorf("failed to get follower chat IDs: %w", err)
	}

	// Use a wait group to synchronize goroutines
	var wg sync.WaitGroup
	ratings := make([]*rating.Rating, len(followerChatIds))
	errCh := make(chan error, len(followerChatIds))
	ratingCh := make(chan *rating.Rating, len(followerChatIds))

	for i, chatId := range followerChatIds {
		wg.Add(1)
		go func(i int, chatId string) {
			defer wg.Done()

			// Get chat history
			chatHistory, err := server.chatState.getChatHistory(chatId, chatServerAddr)
			if err != nil {
				errCh <- fmt.Errorf("failed to get chat history for chat ID %s: %w", chatId, err)
				return
			}

			// Carry out the chat to reach a decision
			rating, err := server.concludeChats(chatId, chatHistory, chatServerAddr, backendURLs[chatServerAddr])
			if err != nil {
				errCh <- fmt.Errorf("failed to carry out chat for chat ID %s: %w", chatId, err)
				return
			}

			// Send the rating to the channel
			ratingCh <- rating
		}(i, chatId)
	}

	// Wait for all goroutines to complete
	wg.Wait()
	close(ratingCh)
	close(errCh)

	// Populate the ratings slice in order
	index := 0
	for rating := range ratingCh {
		ratings[index] = rating
		index++
	}

	// Collect errors, if any
	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("encountered errors while synchronizing chats: %v", errs)
	}

	return ratings, nil
}

// concludeChats ensures the chat reaches a decision state.
func (server *Server) concludeChats(chatId string, chatHistory []string, serverAddr, chatSvcUrl string) (*rating.Rating, error) {
	if len(chatHistory) == 0 {
		return nil, fmt.Errorf("empty chat history for chatID %s", chatId)
	}

	// Search chatHistory backwards for a decision or error
	// The chatHistory alternates between client and server messages (client at even indices, server at odd indices)
	for i := len(chatHistory) - 1; i >= 0; i-- {
		// Skip client messages (even indices, since client starts the chat)
		if i%2 == 0 {
			continue
		}

		response := chatHistory[i]

		// If a decision is found, return it
		if server.isDecision(response) {
			return rating.ParseFromDecision(response)
		}

		// If an error response is found, return an error
		if server.isErrorResponse(response) {
			return nil, fmt.Errorf("error found in chat history for chatID %s: %s", chatId, response)
		}
	}

	// If no decision was found in the history, carry out the chat to reach a decision
	// Initialize chatResp with the last response from chatHistory
	chatResp := BackendChatResponse{
		ChatResponse: ChatResponse{
			Chat: chatHistory[len(chatHistory)-1],
		},
	}

	for !server.isLastCallResponse(chatResp.Chat) {
		if server.isGoodbyeResponse(chatResp.Chat) {
			return nil, fmt.Errorf("unexpected end of conversation for chatID %s", chatId)
		}

		if server.isErrorResponse(chatResp.Chat) {
			return nil, fmt.Errorf("unexpected error in conversation for chatID %s", chatId)
		}

		// Send "no more info" to fast-forward the conversation
		chatResp = server.sendChatRequest(serverAddr, chatSvcUrl, chatId, "no more info")
		if server.isDecision(chatResp.Chat) {
			return rating.ParseFromDecision(chatResp.Chat)
		}
	}

	// Send "no" to trigger the final decision
	decisionResp := server.sendChatRequest(serverAddr, chatSvcUrl, chatId, "no")
	if !server.isDecision(decisionResp.Chat) {
		return nil, fmt.Errorf("failed to reach decision for chatID %s", chatId)
	}

	return rating.ParseFromDecision(decisionResp.Chat)
}

// sendChatRequest sends a chat message to the backend server and returns the response.
func (server *Server) sendChatRequest(serverAddr, chatSvcUrl, chatID, chatMsg string) BackendChatResponse {
	respChan := make(chan BackendChatResponse, 1)
	var wg sync.WaitGroup

	wg.Add(1)
	go server.chatWorker(&wg, serverAddr, chatSvcUrl, chatID, ChatRequest{Chat: chatMsg, ChatID: chatID}, respChan)

	wg.Wait()
	close(respChan)

	resp := <-respChan
	if resp.Err != nil {
		log.Printf("Error sending chat for chat ID %s: %v\n", chatID, resp.Err)
	}

	return resp
}