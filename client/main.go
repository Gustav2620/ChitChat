package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	//"chitchat/client/grpc/chitchat"

	"google.golang.org/grpc"
)

func main() {
	id := flag.String("id", "client1", "client id")
	name := flag.String("name", "Client", "display name")
	addr := flag.String("addr", "localhost:5050", "server addr")
	flag.Parse()

	conn, err := grpc.Dial(*addr, grpc.WithInsecure())
	if err != nil {
		log.Fatalf("dial: %v", err)

	}
	defer conn.Close()

	client := pb.NewChitChatServiceClient(conn)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stream, err := client.Connect(ctx)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}

	if err := stream.Send(&pb.ClientEvent{
		Payload: &pb.ClientEvent_Join{
			Join: &pb.JoinRequest{
				ClientId:    *id,
				DisplayName: *name,
			},
		},
	}); err != nil {
		log.Fatalf("send join: %v", err)
	}

	go func() {
		for {
			ev, err := stream.Recv()
			if err != nil {
				log.Printf("[%s] recv error: %v", *id, err)
				return
			}
			switch p := ev.Payload.(type) {
			case *pb.ServerEvent_Chat:
				fmt.Printf("[%s] CHAT %s: %s\n", *id, p.Chat.ClientId, p.Chat.Content)
			case *pb.ServerEvent_JoinNotification:
				fmt.Printf("[%s] JOIN %s\n", *id, p.JoinNotification.ClientId)
			case *pb.ServerEvent_LeaveNotification:
				fmt.Printf("[%s] LEAVE %s\n", *id, p.LeaveNotification.ClientId)
			default:
			}
		}
	}()

	scanner := bufio.NewScanner(os.Stdin)
	fmt.Printf("[%s] Type messages (empty line to exit):\n", *id)
	for scanner.Scan() {
		text := scanner.Text()
		if text == "" {
			stream.Send(&pb.ClientEvent{
				Payload: &pb.ClientEvent_Leave{
					Leave: &pb.LeaveReqeust{
						ClientId: *id,
					},
				},
			})
			break
		}
		if err := stream.Send(&pb.ClientEvent{
			Payload: &pb.ClientEvent_Chat{
				Chat: &pb.ChatPayload{
					MessageId: fmt.Sprintf("%s-%d", *id, time.Now().UnixNano()),
					ClientId:  *id,
					Content:   text,
					// logical_time left for your Lamport implementation
				},
			},
		}); err != nil {
			log.Printf("send chat err: %v", err)
			break
		}
	}
}
