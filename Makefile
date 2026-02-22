BINARY_NAME=xe-exporter
INSTALL_PATH=/usr/local/bin

all: build

build:
	go build -o $(BINARY_NAME) main.go

install: build
	cp $(BINARY_NAME) $(INSTALL_PATH)/$(BINARY_NAME)
	chmod +x $(INSTALL_PATH)/$(BINARY_NAME)

setup-service:
	cp xe-exporter.service /etc/systemd/system/
	systemctl daemon-reload
	systemctl enable xe-exporter
	systemctl restart xe-exporter

clean:
	rm -f $(BINARY_NAME)
