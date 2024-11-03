build-all: clear-builds build-mac build-windows build-linux

clear-builds:
	rm build/*

build-mac:
	env GOOS=darwin GOARCH=amd64 go build -o=build/asset-market-mac .

build-windows:
	env GOOS=windows GOARCH=amd64 go build -o=build/asset-market-windows.exe .

build-linux:
	env GOOS=linux GOARCH=amd64 go build -o=build/asset-market-linux .

ciBuild: 
	go mod tidy
	env GOOS=linux GOARCH=amd64 go build -o=/workspace/nwc-trading-server .