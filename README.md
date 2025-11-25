# Node 1
go run main.go -mode=node -id=1 -port=8001 -peers=127.0.0.1:8002,127.0.0.1:8003

# Node 2
go run main.go -mode=node -id=2 -port=8002 -peers=127.0.0.1:8001,127.0.0.1:8003

# Node 3
go run main.go -mode=node -id=3 -port=8003 -peers=127.0.0.1:8001,127.0.0.1:8002



Place bid:
# Alice bids 100 through node 1
go run main.go -mode=client -server=127.0.0.1:8001 -clientid=Alice -bid=100

# Bob bids 150 through node 2
go run main.go -mode=client -server=127.0.0.1:8002 -clientid=Bob -bid=150


Query highest bid:
go run main.go -mode=client -server=127.0.0.1:8003 -query=true


Expected output:
Current highest bid: 150 by Bob
