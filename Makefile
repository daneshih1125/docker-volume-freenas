all: build

build:
	go build

install: | docker-volume-freenas
	@echo "### install docker-volume-freenas"
	@mkdir -p /etc/docker-volume-freenas
	@cp docker-volume-freenas.env /etc/docker-volume-freenas/
	@cp docker-volume-freenas /usr/local/bin/
	@cp docker-volume-freenas.service /lib/systemd/system

