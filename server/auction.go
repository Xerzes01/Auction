package main

import (
	"flag"
	"fmt"
	"log"
	"net/rpc"
	"os"
	"sync/atomic"
	"time"

	auction "github.com/Xerzes01/Auction/grpc"
)

var globalClientOpCounter uint64

// ClientBid handles a bid request from a client
func (n *auction.Node) ClientBid(req *auction.BidRequest, reply *auction.BidReply) error {
	n.mu.Lock()
	if !n.isPrimary {
		if n.peerClient == nil {
			n.mu.Unlock()
			reply.Outcome = "exception"
			reply.Reason = "primary not reachable"
			return nil
		}
		n.mu.Unlock()
		return n.peerClient.Call("Node.ClientBid", req, reply)
	}
	n.mu.Unlock()

	current := n.bidders[req.BidderID]
	if req.Amount <= current {
		reply.Outcome = "failed"
		reply.Reason = fmt.Sprintf("bid %d not higher than %e", req.Amount, current)
		return nil
	}

	opID := atomic.AddUint64(&globalClientOpCounter, 1)
	clientID := int64(opID)
	seqNum := time.Now().UnixNano()

	appendArgs := &AppendArgs{
		BidderID: req.BidderID,
		Amount:   req.Amount,
		ClientID: clientID,
		SeqNum:   seqNum,
	}

	var appendReply AppendReply

	n.mu.Lock()
	if n.peerClient == nil {
		n.mu.Unlock()
		reply.Outcome = "exception"
		reply.Reason = "backup not available"
		return nil
	}
	n.mu.Unlock()

	call := n.peerClient.Go("Node.Append", appendArgs, &appendReply, nil)

	select {
	case <-time.After(2 * time.Second):
		reply.Outcome = "exception"
		reply.Reason = "replication timeout"
		return nil
	case <-call.Done:
		if call.Error != nil || !appendReply.Success {
			reply.Outcome = "exception"
			reply.Reason = "backup rejected or unreachable"
			return nil
		}
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	n.log = append(n.log, LogEntry{
		BidderID:  req.BidderID,
		Amount:    req.Amount,
		ClientID:  clientID,
		SeqNum:    seqNum,
		Committed: true,
	})

	n.commitIndex = len(n.log) - 1
	n.clientSeq[clientID] = seqNum
	n.applyUp(n.commitIndex)

	reply.Outcome = "success"
	reply.Reason = "bid accepted and replicated"
	return nil
}

// QueryResult returns the current state of the auction
func (n *Node) QueryResult(_ *StageArgs, reply *QueryReply) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.applyUpTo(n.commitIndex)

	ended := time.Since(n.StartTime) >= AuctionDuration

	reply.AuctionEnded = ended
	reply.HighestAmount = n.HighestAmount
	reply.HighestBidder = n.highestBidder

	if ended {
		if n.highestBidder == "" {
			reply.Message = "Auction ended with no bids"
		} else {
			reply.Message = fmt.Sprintf("Winner: %s with bid %e", n.highestBidder, n.highestAmount)
		}
	} else {
		if n.highestBidder == "" {
			reply.Message = "No bids yet"
		} else {
			reply.Message = "Auction in progress"
		}
	}

	return nil
}

func runClientMode(serverAddr, clientID string, bid int, doQuery bool) {
	client, err := rpc.Dial("tcp", serverAddr)
	if err != nil {
		fmt.Printf("dial error: %v\n", err)
		return
	}
	defer client.Close()

	if doQuery {
		var qr QueryReply
		err = client.Call("Node.QueryResult", StateArgs{}, &qr)
		if err != nil {
			fmt.Printf("query rpc error: %v\n", err)
			return
		}

		if qr.AuctionEnded {
			fmt.Printf("AUCTION ENDED. Winner: %s amount=%e. Message: %s\n",
				qr.HighestBidder, qr.HighestAmount, qr.Message)
		} else {
			fmt.Printf("AUCTION ONGOING. Current highest: %s amount=%e. Message: %s\n",
				qr.HighestBidder, qr.HighestAmount, qr.Message)
		}
		return
	}

	req := BidRequest{
		BidderID: clientID,
		Amount:   bid,
	}

	var rep BidReply
	err = client.Call("Node.ClientBid", req, &rep)
	if err != nil {
		fmt.Printf("ClientBid rpc error: %v\n", err)
		return
	}

	fmt.Printf("Bid outcome: %s. Reason: %s\n", rep.Outcome, rep.Reason)
}

func main() {
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

	if *mode == "client" {
		runClientMode(*serverAddr, *clientID, *bidAmt, *query)
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
	err = node.StartServer()
	if err 	!= nil {
		log.Fatalf("Node server start failed: %v", err)
	}

	time.Sleep(200 * time.Millisecond)
	node.SyncAtStartup()

	log.Printf("[%s] Auction started duration=%s end=%s",
		node.ID, dur, node.StartTime.Add(dur))

	<-time.After(dur + 10*time.Second)
	node.Stop()
	os.Exit(0)
}