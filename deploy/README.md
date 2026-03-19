# Deploy: Linux systemd service

## 1. Create a dedicated user

```bash
sudo useradd -r -s /sbin/nologin -d /var/lib/monitor monitor
sudo mkdir -p /var/lib/monitor /etc/monitor
sudo chown monitor:monitor /var/lib/monitor
```

## 2. Install the binary

Build and copy the binary to the server:

```bash
make build
scp build/monitor user@host:/usr/local/bin/monitor
ssh user@host "chmod +x /usr/local/bin/monitor"
```

## 3. Place the sources config

```bash
scp configs/sources.json user@host:/etc/monitor/sources.json
```

## 4. Create the secrets file

On the server:

```bash
sudo tee /etc/monitor/env > /dev/null <<EOF
MONITOR_TELEGRAM_TOKEN=your_bot_token_here
MONITOR_TELEGRAM_ROOT_CHAT_ID=-1001234567890
EOF
sudo chmod 600 /etc/monitor/env
sudo chown root:root /etc/monitor/env
```

## 5. Install and start the service

```bash
sudo cp deploy/monitor.service /etc/systemd/system/monitor.service
sudo systemctl daemon-reload
sudo systemctl enable --now monitor
```

## 6. Verify

```bash
systemctl status monitor
journalctl -u monitor -f
```

## Updating the binary

The CI/CD pipeline handles this automatically on tagged releases.
For a manual update:

```bash
systemctl stop monitor
scp build/monitor user@host:/usr/local/bin/monitor
systemctl start monitor
systemctl is-active monitor
```
