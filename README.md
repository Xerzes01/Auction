# Start-up
Terminal 1:
go run main.go -mode=node -id=1 -port=8001 -peers=localhost:8002,localhost:8003

Terminal 2:
go run main.go -mode=node -id=2 -port=8002 -peers=localhost:8001,localhost:8003

Terminal 3:
go run main.go -mode=node -id=3 -port=8003 -peers=localhost:8001,localhost:8002


# Commands
Place bid:
bid x

Query highest bid:
Result

Help:
Help

Leave:
Exit


After 100 seconds, a winner will be decided depending on the highest bid.
