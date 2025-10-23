package main

import (
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"ChitChat/client/grpc/chitchat"
	"sync"

	"google.golang.org/grpc"
)

type server struct {
	chitchat.UnimplementedChitChatServiceServer
	clients      map[string]chitchat.ChitChatService_ChatServer
	clientsMu    sync.RWMutex
	logicalClock int64
	clockMu      sync.RWMutex
	logger       *slog.Logger
}

func main() {
	// Structured loggin setup
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level:     slog.LevelInfo,
		AddSource: true,
	}))
	logger.Info("Server starting", "port", ":50051") //... should be replaced with a port

	//grpc server setup
	lis, err := net.Listen("tcp", ":50051") //same as above
	if err != nil {
		logger.Error("Failed to listen", "error", err)
		os.Exit(1)
	}

	s := grpc.NewServer()
	chitchat.RegisterChitChatServiceServer(s, &server{
		clients:      make(map[string]chitchat.ChitChatService_ChatServer),
		logger:       logger,
		logicalClock: 0,
	})

	//Start gorutine
	go func() {
		if err := s.Serve(lis); err != nil {
			logger.Error("Failed to serve", "error", err)
			os.Exit(1)
		}
	}()

	logger.Info("Server started successfully", "port", ":50051") //same as above

	//Shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Info("Server shutting down")
	s.GracefulStop()
	logger.Info("Server shutdown complete")
}
