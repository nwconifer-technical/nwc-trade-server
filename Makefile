build-all: clear-builds build-mac build-windows build-linux

clear-builds:
	rm build/*

build-mac:
	env GOOS=darwin GOARCH=amd64 go build -o=build/asset-market-mac .

build-windows:
	env GOOS=windows GOARCH=amd64 go build -o=build/asset-market-windows.exe .

build-linux:
	env GOOS=linux GOARCH=amd64 go build -o=build/asset-market-linux .

buildEnvironment:
	export CASH_TRANSACT_COLL="$(gcloud secrets versions access latest --secret="CASH_TRANSACT_COLL")"
	export DB_CONNECTSTRING="$(gcloud secrets versions access latest --secret="DB_CONNECTSTRING")"
	export HASH_COST="$(gcloud secrets versions access latest --secret="HASH_COST")"
	export EXTRA_KEY_STRING="$(gcloud secrets versions access latest --secret="EXTRA_KEY_STRING")"
	export FIRESTORE_DB="$(gcloud secrets versions access latest --secret="FIRESTORE_DB")"
	export PROJECT_ID="$(gcloud config get-value project)"

localBuild:
	env GOOS=linux GOARCH=amd64 go build -o=./nwc-trading-server -ldflags="-X 'main.HashCost=${HASH_COST}' -X 'main.DbString=${DB_CONNECTSTRING}' -X 'main.ProjId=nsinvest-infra' -X 'main.ExtraKeyString=${EXTRA_KEY_STRING}' -X 'main.FirestoreString=${FIRESTORE_DB}'" .

ciBuild: 
	go mod tidy
	env GOOS=linux GOARCH=amd64 go build -o=/workspace/nwc-trading-server -ldflags="-X 'main.HashCost=${HASH_COST}' -X 'main.DbString=${DB_CONNECTSTRING}' -X 'main.ProjId=nsinvest-infra' -X 'main.ExtraKeyString=${EXTRA_KEY_STRING}' -X 'main.FirestoreString=${FIRESTORE_DB}'" .