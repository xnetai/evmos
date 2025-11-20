# Production Load Test Setup Guide

This guide explains how to run the load tests with actual separate validator processes, simulating a production environment.

## Overview

The `TestBatchSize100k2ssProd` test is designed to simulate a production environment where each validator runs in a separate process with its own port bindings. This provides a more realistic test environment compared to the in-process integration tests.

## Quick Start (Using Existing Testnet Setup)

### Step 1: Initialize Validator Configuration Files

Use the `evmosd testnet init-files` command to create configuration directories for 4 validators:

```bash
# Navigate to evmos directory
cd /home/user/evmos

# Initialize 4 validator nodes
evmosd testnet init-files \
  --v 4 \
  --output-dir ./.testnets/loadtest \
  --starting-ip-address 127.0.0.1 \
  --chain-id evmos_9002-1 \
  --keyring-backend test

# This creates:
# .testnets/loadtest/node0/
# .testnets/loadtest/node1/
# .testnets/loadtest/node2/
# .testnets/loadtest/node3/
```

### Step 2: Start Validator Processes

Start each validator in a separate terminal or using a process manager:

```bash
# Terminal 1 - Validator 0
evmosd start --home ./.testnets/loadtest/node0/evmosd \
  --rpc.laddr tcp://0.0.0.0:26657 \
  --p2p.laddr tcp://0.0.0.0:26656 \
  --grpc.address 0.0.0.0:9090 \
  --api.address tcp://0.0.0.0:1317 \
  --json-rpc.address 0.0.0.0:8545

# Terminal 2 - Validator 1
evmosd start --home ./.testnets/loadtest/node1/evmosd \
  --rpc.laddr tcp://0.0.0.0:26667 \
  --p2p.laddr tcp://0.0.0.0:26666 \
  --grpc.address 0.0.0.0:9100 \
  --api.address tcp://0.0.0.0:1327 \
  --json-rpc.address 0.0.0.0:8555

# Terminal 3 - Validator 2
evmosd start --home ./.testnets/loadtest/node2/evmosd \
  --rpc.laddr tcp://0.0.0.0:26677 \
  --p2p.laddr tcp://0.0.0.0:26676 \
  --grpc.address 0.0.0.0:9110 \
  --api.address tcp://0.0.0.0:1337 \
  --json-rpc.address 0.0.0.0:8565

# Terminal 4 - Validator 3
evmosd start --home ./.testnets/loadtest/node3/evmosd \
  --rpc.laddr tcp://0.0.0.0:26687 \
  --p2p.laddr tcp://0.0.0.0:26686 \
  --grpc.address 0.0.0.0:9120 \
  --api.address tcp://0.0.0.0:1347 \
  --json-rpc.address 0.0.0.0:8575
```

### Step 3: Run Load Tests

Once all validators are running, execute the load test:

```bash
cd tests/loadtest
go test -v -run TestBatchSize100k2ssProd -timeout 30m
```

## Automated Setup (Using Docker Compose)

For automated setup, use Docker Compose:

### docker-compose.yml

```yaml
version: '3.8'

services:
  validator0:
    image: evmos/evmos:latest
    container_name: evmos-validator-0
    command: >
      start
      --home /evmos
      --rpc.laddr tcp://0.0.0.0:26657
      --p2p.laddr tcp://0.0.0.0:26656
      --grpc.address 0.0.0.0:9090
      --api.address tcp://0.0.0.0:1317
      --json-rpc.address 0.0.0.0:8545
    volumes:
      - ./.testnets/loadtest/node0/evmosd:/evmos
    ports:
      - "26657:26657"
      - "26656:26656"
      - "9090:9090"
      - "1317:1317"
      - "8545:8545"
    networks:
      - evmos-loadtest

  validator1:
    image: evmos/evmos:latest
    container_name: evmos-validator-1
    command: >
      start
      --home /evmos
      --rpc.laddr tcp://0.0.0.0:26657
      --p2p.laddr tcp://0.0.0.0:26656
      --grpc.address 0.0.0.0:9090
      --api.address tcp://0.0.0.0:1317
      --json-rpc.address 0.0.0.0:8545
    volumes:
      - ./.testnets/loadtest/node1/evmosd:/evmos
    ports:
      - "26667:26657"
      - "26666:26656"
      - "9100:9090"
      - "1327:1317"
      - "8555:8545"
    networks:
      - evmos-loadtest

  validator2:
    image: evmos/evmos:latest
    container_name: evmos-validator-2
    command: >
      start
      --home /evmos
      --rpc.laddr tcp://0.0.0.0:26657
      --p2p.laddr tcp://0.0.0.0:26656
      --grpc.address 0.0.0.0:9090
      --api.address tcp://0.0.0.0:1317
      --json-rpc.address 0.0.0.0:8545
    volumes:
      - ./.testnets/loadtest/node2/evmosd:/evmos
    ports:
      - "26677:26657"
      - "26676:26656"
      - "9110:9090"
      - "1337:1317"
      - "8565:8545"
    networks:
      - evmos-loadtest

  validator3:
    image: evmos/evmos:latest
    container_name: evmos-validator-3
    command: >
      start
      --home /evmos
      --rpc.laddr tcp://0.0.0.0:26657
      --p2p.laddr tcp://0.0.0.0:26656
      --grpc.address 0.0.0.0:9090
      --api.address tcp://0.0.0.0:1317
      --json-rpc.address 0.0.0.0:8545
    volumes:
      - ./.testnets/loadtest/node3/evmosd:/evmos
    ports:
      - "26687:26657"
      - "26686:26656"
      - "9120:9090"
      - "1347:1317"
      - "8575:8545"
    networks:
      - evmos-loadtest

networks:
  evmos-loadtest:
    driver: bridge
```

### Start with Docker Compose

```bash
# Start all validators
docker-compose up -d

# View logs
docker-compose logs -f

# Stop all validators
docker-compose down
```

## Port Allocation Strategy

The production test uses the following port allocation per validator:

| Validator | RPC Port | P2P Port | gRPC Port | API Port | JSON-RPC Port |
|-----------|----------|----------|-----------|----------|---------------|
| 0         | 26657    | 26656    | 9090      | 1317     | 8545          |
| 1         | 26667    | 26666    | 9100      | 1327     | 8555          |
| 2         | 26677    | 26676    | 9110      | 1337     | 8565          |
| 3         | 26687    | 26686    | 9120      | 1347     | 8575          |

Formula: `base_port + (validator_index * 10)`

## Monitoring

### Check Validator Status

```bash
# Check validator 0
curl http://localhost:26657/status

# Check validator 1
curl http://localhost:26667/status

# Check all validators
for port in 26657 26667 26677 26687; do
  echo "Validator on port $port:"
  curl -s http://localhost:$port/status | jq .result.sync_info
done
```

### Monitor Socket Connections

```bash
# View listening ports
ss -ltn | grep -E "26657|26667|26677|26687|26656|26666|26676|26686"

# View established connections
ss -tn state established | grep -E "26656|26666|26676|26686"

# Count connections per validator
for port in 26656 26666 26676 26686; do
  count=$(ss -tn state established | grep ":$port" | wc -l)
  echo "Validator P2P port $port: $count connections"
done
```

### View Logs

```bash
# If running manually
tail -f ./.testnets/loadtest/node0/evmosd/evmosd.log

# If using Docker Compose
docker-compose logs -f validator0
```

## Production Deployment Checklist

### Pre-Deployment

- [ ] Initialize validator configuration files
- [ ] Review and customize genesis parameters
- [ ] Set up persistent peer connections
- [ ] Configure firewall rules for P2P ports
- [ ] Set up monitoring and alerting
- [ ] Prepare backup and recovery procedures

### Deployment

- [ ] Start validators in sequence
- [ ] Verify network connectivity
- [ ] Confirm all validators are syncing
- [ ] Check block production
- [ ] Verify P2P connections between validators

### Post-Deployment

- [ ] Monitor validator status
- [ ] Check block height synchronization
- [ ] Verify transaction processing
- [ ] Monitor resource usage (CPU, memory, disk, network)
- [ ] Set up log rotation
- [ ] Configure backups

## Troubleshooting

### Validators Not Connecting

1. Check firewall rules:
   ```bash
   sudo ufw status
   ```

2. Verify P2P addresses in config:
   ```bash
   cat ./.testnets/loadtest/node0/evmosd/config/config.toml | grep persistent_peers
   ```

3. Check if ports are in use:
   ```bash
   sudo lsof -i :26656
   ```

### Validators Not Syncing

1. Check validator logs:
   ```bash
   tail -f ./.testnets/loadtest/node0/evmosd/evmosd.log
   ```

2. Verify genesis file matches across all validators:
   ```bash
   sha256sum ./.testnets/loadtest/node*/evmosd/config/genesis.json
   ```

3. Check block height:
   ```bash
   curl localhost:26657/status | jq .result.sync_info.latest_block_height
   ```

### High Resource Usage

1. Monitor CPU and memory:
   ```bash
   top -p $(pgrep evmosd)
   ```

2. Check disk I/O:
   ```bash
   iostat -x 5
   ```

3. Monitor network bandwidth:
   ```bash
   iftop
   ```

## Performance Tuning

### Operating System

```bash
# Increase file descriptor limits
ulimit -n 65536

# Optimize TCP settings
sudo sysctl -w net.core.rmem_max=134217728
sudo sysctl -w net.core.wmem_max=134217728
sudo sysctl -w net.ipv4.tcp_rmem="4096 87380 134217728"
sudo sysctl -w net.ipv4.tcp_wmem="4096 65536 134217728"
```

### CometBFT Configuration

Edit `config.toml` for each validator:

```toml
[consensus]
timeout_propose = "3s"
timeout_prevote = "1s"
timeout_precommit = "1s"
timeout_commit = "5s"

[mempool]
size = 10000
cache_size = 20000

[p2p]
max_num_inbound_peers = 40
max_num_outbound_peers = 10
send_rate = 20000000
recv_rate = 20000000
```

## Security Considerations

1. **Network Isolation**: Use separate VLANs or security groups for validator communication
2. **Firewall Rules**: Only expose necessary ports
3. **Key Management**: Store validator keys securely
4. **DDoS Protection**: Implement rate limiting and connection limits
5. **Monitoring**: Set up alerts for suspicious activity

## References

- [Evmos Documentation](https://docs.evmos.org)
- [CometBFT Documentation](https://docs.cometbft.com)
- [Cosmos SDK Documentation](https://docs.cosmos.network)
