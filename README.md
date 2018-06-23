# Docker Volume Plugin for FreeNAS

## Environment 

* Ubuntu 16.04
* FreeNAS-9.10-RELEASE

## Setup

### preparation
```
sudo apt-get install -y open-iscsi
```

### Build

```bash
make
```

### Install

```bash
sudo make install
```

### Configuration
***/etc/docker-volume-freenas/docker-volume-freenas.env***

```
FREENAS_API_URL=http://192.168.67.68
FREENAS_API_USER=root
FREENAS_API_PASSWORD=freenas
```

### Usage
1 - Create 1G volume

```bash
sudo docker volume create -d freenas -o size=1 freenas001
```

2 - Run container and touch files

```
sudo docker run -it -v freenas001:/www busybox touch /www/{a,b,c}
```

3 - Verify

```
sudo docker run -it -v freenas001:/www busybox ls -l  /www/
```
