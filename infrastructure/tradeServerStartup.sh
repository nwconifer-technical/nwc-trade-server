echo "Rebuilding Machine!"
git clone git@github.com:nwconifer-technical/nwc-trade-server.git
echo $USER
cd nwc-trade-server/
make build-linux
cp -f infrastructure/systemd/trade-api.service /etc/systemd/system/trade-api.service
systemctl daemon-reload
systemctl restart trade-server
cd .. && rm nwc-trade-server
echo "Machine Rebuilt!"