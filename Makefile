(HASH_COST) = "$(gcloud secrets versions access latest --secret="HASH_COST")"
(EXTRA_KEY_STRING) = "$(gcloud secrets versions access latest --secret="EXTRA_KEY_STRING")"
(DB_CONNECTSTRING) = "$(gcloud secrets versions access latest --secret="DB_CONNECTSTRING")"

localBuild: 
	echo $(HASH_COST)
	env GOOS=linux GOARCH=amd64 go build -o=./nwc-trading-server -ldflags="-X 'main.HashCost=$(HASH_COST)' -X 'main.DbString=$(DB_CONNECTSTRING)' -X 'main.ExtraKeyString=$(EXTRA_KEY_STRING)'" .

localWithLive:
	env GOOS=linux GOARCH=amd64 go build -o=./nwc-trading-server -ldflags="-X 'main.HashCost=${HASH_COST}' -X 'main.DbString=${DB_CONNECTSTRING}' -X 'main.ExtraKeyString=${EXTRA_KEY_STRING}'" .

ciBuild: 
	go mod tidy
	env GOOS=linux GOARCH=amd64 go build -o=/workspace/nwc-trading-server -ldflags="-X 'main.HashCost=${HASH_COST}' -X 'main.DbString=${DB_CONNECTSTRING}' -X 'main.ExtraKeyString=${EXTRA_KEY_STRING}'" .