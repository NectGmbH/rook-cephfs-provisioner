TAG=0.1.3
IMAGE=kavatech/rook-cephfs-provisioner

export GOOS=linux

all: build docker-build

build:
	go build

docker-build:
	docker build -t $(IMAGE):$(TAG) .

docker-push:
	docker push $(IMAGE):$(TAG)
