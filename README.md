# Scale to zero

DNS server and container orchestrator that starts/stops container on demand to keep memory use low.

```bash
sudo podman build -t=spring-service .
```

fix internet access from container:

```bash
sudo sysctl -w net.ipv4.conf.all.forwarding=1
sudo iptables -P FORWARD ACCEPT
```

```bash
# cleanup
sudo podman container ls -aq | xargs sudo podman container stop
sudo podman pod rm -f debug-pod
sudo podman container ls -aq | xargs sudo podman container rm
sudo podman network ls -q | xargs sudo podman network rm
# create
sudo podman network create --subnet 172.20.0.0/24 --disable-dns internal-proxy-net
sudo podman pod create --name debug-pod --network internal-proxy-net -p 80:80 -p 53:53/udp -p 2345:2345
sudo podman run -d --pod debug-pod --name network-holder alpine sleep infinity
set PID $(sudo podman inspect network-holder --format '{{.State.Pid}}')

# Run Delve inside the network namespace
sudo nsenter -t $PID -n -i -u go run cmd/proxy/main.go -mod=vendor
sudo nsenter -t $PID -n -i -u dlv debug --headless --listen=:2345 --api-version=2 cmd/proxy/main.go -- -mod=vendor
# I had some network issues on train, so I messed around with some parameters here:
#sudo GOPROXY=off nsenter -t $PID -n -i -u dlv debug --headless --listen=:2345 --api-version=2 cmd/proxy/main.go -- -mod=vendor -tags "exclude_graphdriver_btrfs"
```

```bash
sudo podman exec -it network-holder /bin/sh
sudo podman exec -it orders-service-container /bin/sh
# make dns server preload/start container on dns lookup
dig @localhost orders-service
# stress test memory use (note: systemd-oom may kill other processes first)
stress-ng --vm 4 --vm-bytes 16G --timeout 10s
# on direct http request (via proxy container ip) we also want to start the required container
curl 172.20.0.3
curl 172.20.0.5
sudo podman container stop orders-service-container
sudo podman container checkpoint --tcp-estblished inventory-service-container
sudo podman container restore --tcp-established inventory-service-container
```
