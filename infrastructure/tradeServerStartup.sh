echo "Rebuilding Machine!"
source /etc/profile
export CASH_TRANSACT_COLL="$(gcloud secrets versions access latest --secret="CASH_TRANSACT_COLL")"
export DB_CONNECTSTRING="$(gcloud secrets versions access latest --secret="DB_CONNECTSTRING")"
export HASH_COST="$(gcloud secrets versions access latest --secret="HASH_COST")"
export EXTRA_KEY_STRING="$(gcloud secrets versions access latest --secret="EXTRA_KEY_STRING")"
export FIRESTORE_DB="$(gcloud secrets versions access latest --secret="FIRESTORE_DB")"
export PROJECT_ID="$(gcloud config get-value project)"
systemctl daemon-reload
systemctl restart trade-server
cd .. && rm -rf nwc-trade-server
echo "INFO: Machine Rebuilt!"