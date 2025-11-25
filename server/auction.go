package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"
	//auction "github.com/Xerzes01/Auction/grpc";
)


func runClientMode(serverAddr, clientID string, bid int, doQuery bool){
	client, err := rpc.dial("tcp", serverAddr)
	if err != nil {
		fmt.Printf("dial error: %v\n", err)
		return
	}
	defer client.close()

	if doQuery {
		var qr QueryReply
		err = client.Call("Node.QueryResult", StateArgs{}, &qr)
		if err != nil {
			fmt.Printf("query rpc error: %v\n", err)
			return
		}

		if qr.AuctionEnded {
			if qr.AuctionEnded {
			fmt.Printf("AUCTION ENDED. Winner: %s amount=%d. Message: %s\n",
				qr.HighestBidder, qr.HighestAmount, qr.Message)
		} else {
			fmt.Printf("AUCTION ONGOING. Current highest: %s amount=%d. Message: %s\n",
				qr.HighestBidder, qr.HighestAmount, qr.Message)
		}
		return
		}
	}

	req := BidRequest{BidderID: clientID, Amount: bid}
	var rep BidReply
	err = client.Call("Node.ClientBid", req, &rep)
	if err != nil {
		fmt.Printf("ClientBid rpc error: %v\n", err)
		return
	}
	fmt.Printf("Bid outcome: %s. Reason: %s\n", rep.Outcome, rep.Reason)
}

func main(){
	mode := flag.String("mode", "node", "mode: node or client")
	
	id := flag.String("id", "node1", "node id")
	port := flag.Int("port", 8001, "TCP port")
	peersStr := flag.String("peers", "", "comma-separated peer host:port list")
	durationStr := flag.String("duration", "100s", "auction duration")

	serverAddr := flag.String("server", "127.0.0.1:8001", "server address")
	clientID := flag.String("clientid", "client1", "client id")
	bidAmt := flag.Int("bid", 0, "bid amount")
	query := flag.Bool("query", false, "query mode")

	flag.Parse()

	if *mode =="client"{
		runClientMode(*serverAddr,*clientID,*bidAmt,*query)
		return
	}

	if *id != "0" && *id != "1" {
		log.Fatal("id must be 0 or 1")
	}

	peers := parsePeers(*peersStr)
	dur, err := time.ParseDuration(*durationStr)
	if err != nil {
		fmt.Printf("Bad duration: %v\n", err)
		return
	}

	node := NewNode(*id, *port, peers, dur)
	err := node.StartServer()
	if err != nil {
		log.Fatalf("Node server start failed: %v", err)
	}

	time.Sleep(200 * time.Millisecond)
	node.SyncAtStartup()

	log.Printf("[%s] Auction started duration=%s end=%s", node.ID, dur, node.StartTime.Add(dur))
	<-time.After(dur + 10*time.Second)
	
	node.Stop()
	os.Exit(0)

}