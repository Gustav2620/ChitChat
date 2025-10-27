package main

import (
	"fmt"
	"time"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	chitchat "github.com/Gustav2620/ChitChat/grpc"
	
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
	port := ":50051"
	logger.Info("Server starting", "port", port) //... should be replaced with a port

	//grpc server setup
	lis, err := net.Listen("tcp", port) //same as above
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

	logger.Info("Server started successfully", "port", port) //same as above

	//Shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Info("Server shutting down")
	s.GracefulStop()
	logger.Info("Server shutdown complete")
}

//CHAT FUNCTION
func (s *server) Chat(stream chitchat.ChitChatService_ChatServer) error {
	ctx := stream.Context()
	clientID := generateClientID()

	s.logger.Info("Client connected",
		slog.String("client_id", clientID),
		slog.String("event", "connection"))

	s.clientsMu.Lock()
	if _, exists := s.clients[clientID]; exists {
		clientID = clientID + "-" + generateClientID()
	}
	s.clients[clientID] = stream
	s.clientsMu.Unlock()
	s.logger.Info("Client connected", "component", "Server", "event", "connection", "client_id", clientID)

	s.incrementClock()
	joinMsg := &chitchat.ChatMessage{
		ClientId:    clientID,
		Content:     fmt.Sprintf("Participant %s joined Chit Chat at logical time %d", clientID, s.logicalClock),
		LogicalTime: s.getClock(),
		Type:        chitchat.MessageType_JOIN,
	}
	s.broadcast(joinMsg)

	msgChan := make(chan *chitchat.ChatMessage)
	errChan := make(chan error, 1)

	go func() {
		for {
			msg, err := stream.Recv()
			if err != nil {
				errChan <- err
				return
			}
			msgChan <- msg
		}
	}()

	for {
		select {
		case msg := <-msgChan:
			if msg == nil {
				continue
			}
			if msg.Type == chitchat.MessageType_CHAT {
				// enforce max length 128
				if len(msg.Content) > 128 {
					s.logger.Warn("Message too long; ignoring", "component", "Server", "client_id", clientID, "length", len(msg.Content))
					continue
				}
			}

			// Update server clock using Lamport rule
			s.updateClockOnReceive(msg.LogicalTime)

			// Stamp outgoing broadcast with server's logical time
			s.incrementClock()
			broadcast := &chitchat.ChatMessage{
				ClientId:    clientID,
				Content:     msg.Content,
				LogicalTime: s.getClock(),
				Type:        msg.Type,
			}
			s.broadcast(broadcast)
		case err := <-errChan:
			if err != nil {
				s.logger.Info("Client disconnected", "component", "Server", "event", "disconnection", "client_id", clientID, "error", err)
				// broadcast LEAVE
				s.incrementClock()
				leaveMsg := &chitchat.ChatMessage{
					ClientId:    clientID,
					Content:     fmt.Sprintf("Participant %s left Chit Chat", clientID),
					LogicalTime: s.getClock(),
					Type:        chitchat.MessageType_LEAVE,
				}
				s.broadcast(leaveMsg)
				s.removeClient(clientID)
				return nil
			}
		case <-ctx.Done():
			s.logger.Info("Client context done", "component", "Server", "client_id", clientID)
			// broadcast LEAVE
			s.incrementClock()
			leaveMsg := &chitchat.ChatMessage{
				ClientId:    clientID,
				Content:     fmt.Sprintf("Participant %s left Chit Chat", clientID),
				LogicalTime: s.getClock(),
				Type:        chitchat.MessageType_LEAVE,
			}
			s.broadcast(leaveMsg)
			s.removeClient(clientID)
			return nil
		}
	}
}

func (s *server) handleIncomingMessage(clientID string, msg *chitchat.ChatMessage){
	s.logger.Info("Message received",
		slog.String("Client ID", clientID),
		slog.String("Content", msg.Content),
		slog.String("Type", msg.Type.String()))
	
	s.updateClockOnReceive(msg.LogicalTime)


	if(msg.Type == chitchat.MessageType_CHAT && len(msg.Content) > 128) {
		s.logger.Warn("Message too long",
			slog.String("Client ID", clientID),
			slog.Int("Lenght", len(msg.Content)))
		return
	}

	s.broadcast(msg)
}

func (s *server) getLogicalTime() int64{
	s.clockMu.RLock()
	defer s.clockMu.RUnlock()
	return s.logicalClock
}

func (s *server) broadcastJoin(clientID string) {
	msg := &chitchat.ChatMessage{
		ClientId:	clientID,
		Content:	fmt.Sprintf("%d joined chat", clientID),
		LogicalTime: s.getLogicalTime(),
		Type:		chitchat.MessageType_JOIN,
	}

	s.updateClockOnReceive(msg.LogicalTime)
	s.broadcast(msg)
	s.logger.Info("Join received",
		slog.String("Client ID", clientID),
		slog.Int64("Logical time", msg.LogicalTime))
}

func (s *server) broadcastLeave(clientID string) {
	msg := &chitchat.ChatMessage{
		ClientId:	clientID,
		Content:	fmt.Sprintf("%d left chat", clientID),
		LogicalTime: s.getLogicalTime(),
		Type:		chitchat.MessageType_LEAVE,
	}

	s.updateClockOnReceive(msg.LogicalTime)
	s.broadcast(msg)
	s.logger.Info("Leave received",
		slog.String("Client ID", clientID),
		slog.Int64("Logical time", msg.LogicalTime))
}

func (s *server) broadcast(msg *chitchat.ChatMessage){
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

	wg := sync.WaitGroup{}
	for clientID, stream := range s.clients{
		wg.Add(1)
		go func(cid string, st chitchat.ChitChatService_ChatServer){
			defer wg.Done()
			if err := st.Send(msg); err != nil {
				s.logger.Warn("Failed to send to client",
					slog.String("Client ID", cid),
					slog.String("error", err.Error()))
				return
			}
			s.logger.Info("Message delivered",
				slog.String("To client", cid),
				slog.String("From client", msg.ClientId),
				slog.Int64("Logical time", msg.LogicalTime),
				slog.String("Type", msg.Type.String()))
		} (clientID, stream)
	}
	wg.Wait()
}

func (s *server) removeClient(clientID string) {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()
	delete(s.clients, clientID)
}

func generateClientID() string{
	return fmt.Sprintf("client_%d", time.Now().UnixNano()%1000000)
}

func (s *server) incrementClock() {
    s.clockMu.Lock()
    s.logicalClock++
    s.clockMu.Unlock()
}

func (s *server) getClock() int64 {
    s.clockMu.RLock()
    defer s.clockMu.RUnlock()
    return s.logicalClock
}

func (s *server) updateClockOnReceive(received int64) {
    s.clockMu.Lock()
    if received > s.logicalClock {
        s.logicalClock = received
    }
    s.logicalClock++
    s.clockMu.Unlock()
}
