package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"auction/nodes"
	pb "github.com/Xerzes01/Auction/grpc"
)

func main() {
	mode := flag.String("mode", "node", "Run mode: node or client")
	id := flag.Int("id", 0, "Node ID")
	port := flag.Int("port", 8001, "Port for gRPC server")
	serverAddr := flag.String("server", "127.0.0.1:8001", "Server address for clients")
	peers := flag.String("peers", "", "Comma-separated peer node addresses")

	clientID := flag.String("clientid", "", "Client name")
	bidAmount := flag.Int("bid", 0, "Bid amount")
	query := flag.Bool("query", false, "Query winning bid")

	flag.Parse()

	if *mode == "node" {
		peerList := nodes.ParsePeers(*peers)
		node := nodes.NewAuctionNode(*id, *port, peerList)
		node.Start()
	} else if *mode == "client" {
		conn, client := nodes.ConnectToNode(*serverAddr)
		if conn == nil {
			log.Fatal("Failed to connect to server")
		}
		defer conn.Close()

		ctx, cancel := context.WithTimeout(context.Background(), time.Second*2)
		defer cancel()

		if *query {
			res, err := client.GetResult(ctx, &pb.Empty{})
			if err != nil {
				log.Println("Query failed:", err)
				return
			}
			fmt.Printf("Current highest bid: %d by %s\n", res.Amount, res.Bidder)
			return
		}

		if *clientID == "" {
			log.Println("Client ID required to bid")
			os.Exit(1)
		}

		res, err := client.PlaceBid(ctx, &pb.Bid{
			Bidder: *clientID,
			Amount: int32(*bidAmount),
		})
		if err != nil {
			log.Println("Bid failed:", err)
			return
		}

		fmt.Println(res.Message)
	}
}
