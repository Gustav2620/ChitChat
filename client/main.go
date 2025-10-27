package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"time"
	"log/slog"
	"google.golang.org/grpc/credentials/insecure"
	chitchat "github.com/Gustav2620/ChitChat/grpc"

	"google.golang.org/grpc"
)

func main() {
	id := flag.String("id", "client1", "client id")
	name := flag.String("name", "Client", "display name")
	addr := flag.String("addr", "localhost:50051", "server addr")
	flag.Parse()

	if *id == "" {
		fmt.Println("Please provide -id")
		return
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	conn, err := grpc.Dial(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		logger.Error("Failed to dial server", "error", err)
		return
	}
	defer conn.Close()

	client := chitchat.NewChitChatServiceClient(conn)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stream, err := client.Chat(ctx)
	if err != nil {
		logger.Error("Failed to open chat stream", "error", err)
		return
	}

	// Send JOIN first
	joinMsg := &chitchat.ChatMessage{
		ClientId:    *id,
		Content:     fmt.Sprintf("%s", *name),
		LogicalTime: 0,
		Type:        chitchat.MessageType_JOIN,
	}
	if err := stream.Send(joinMsg); err != nil {
		logger.Error("Failed to send JOIN", "error", err)
		return
	}
	logger.Info("Sent JOIN", "component", "Client", "client_id", *id)

	go func() {
		for {
			msg, err := stream.Recv()
			if err != nil {
				logger.Info("Receive error or stream closed", "error", err)
				return
			}
			fmt.Printf("[%s] %s (type=%s, time=%d)\n", *id, msg.Content, msg.Type.String(), msg.LogicalTime)
			logger.Info("Received broadcast", "component", "Client", "client_id", *id, "from", msg.ClientId, "type", msg.Type.String(), "logical_time", msg.LogicalTime, "content", msg.Content)
		}
	}()

	scanner := bufio.NewScanner(os.Stdin)
	fmt.Printf("[%s] Type messages (empty line to exit):\n", *id)
	for scanner.Scan() {
		text := scanner.Text()
		if text == "" {
			leaveMsg := &chitchat.ChatMessage{
				ClientId:    *id,
				Content:     "",
				LogicalTime: 0,
				Type:        chitchat.MessageType_LEAVE,
			}
			if err := stream.Send(leaveMsg); err != nil {
				logger.Error("Failed to send LEAVE", "error", err)
			}
			// allow some time for server to broadcast leave
			time.Sleep(200 * time.Millisecond)
			break
		}

		chatMsg := &chitchat.ChatMessage{
			ClientId:    *id,
			Content:     text,
			LogicalTime: 0, // clients may set their own logical time if desired
			Type:        chitchat.MessageType_CHAT,
		}
		if len(chatMsg.Content) > 128 {
			logger.Warn("Message too long; trimming to 128 chars")
			chatMsg.Content = chatMsg.Content[:128]
		}
		if err := stream.Send(chatMsg); err != nil {
			logger.Error("Failed to send chat", "error", err)
			break
		}
	}
	cancel()
	// wait for the last messages to finish sending
	time.Sleep(100 * time.Millisecond)
	logger.Info("Client exiting", "component", "Client", "client_id", *id)
}
