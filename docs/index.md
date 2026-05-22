# Blockforge
## Getting Started
## Linux
Create a system / nologin user
```bash
sudo useradd \
  --system \
  --home-dir /opt/minecraft \
  --create-home \
  --shell /usr/sbin/nologin \
  minecraft
```
Ensure directory ownership
```bash
sudo chown -R minecraft:minecraft /opt/minecraft
```

Install Blockforge


### OpenRC
- Configure the environment file - [conf.d](openRC.md#conf.d)
- Setup the init script - [init.d](openRC.md#init.d)
