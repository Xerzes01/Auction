package main

import (
	"github.com/Xerzes01/Auction/nodes"
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	mode := flag.String("mode", "", "Mode: node")
	nodeID := flag.Int("id", 0, "Node ID")
	port := flag.String("port", "", "Node port")
	peerList := flag.String("peers", "", "Comma-separated list of peer addresses")
	flag.Parse()

	switch *mode {
	case "node":
		if *nodeID == 0 || *port == "" {
			fmt.Println("Usage: go run main.go -mode=node -id=1 -port=8001 -peers=ip:port,ip:port")
			os.Exit(1)
		}

		var peers []string
		if *peerList != "" {
			peers = strings.Split(*peerList, ",")
		}

		node := nodes.NewNode(*nodeID, *port, peers)

		go node.StartGRPCServer()
		node.StartConsole()      
	default:
		fmt.Println("Unknown or missing mode. Usage:")
		fmt.Println("go run main.go -mode=node -id=1 -port=8001 -peers=host:port,host:port")
	}
}
