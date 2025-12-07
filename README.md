# ssh-wol-proxy

A proxy for SSH which will send a wake-on-lan (WOL) packet on attempted connection. Useful to start a server that may be offline when using a tool such as remote development in VS Code.

Confgiure it with env vars such as follows, replacing the following examples with values relevant to your setup:

```
TARGET_HOST: "192.168.0.100"
TARGET_MAC: "58:11:22:30:6a:f4"
PROXY_HOST: "0.0.0.0"
PROXY_PORT: "2222"
TARGET_PORT: "22"
TARGET_BROADCAST_IP: "192.168.0.110"
CONNECTION_RETRIES: "10"
RETRY_DELAY_SECONDS: "5"
```