.PHONY: build
build: build-bind-mount
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/orchestrator .
	@echo "ask sudo to assign cap_sys_admin to orchestrator"
# note the eip here is necessary
# as we manually call PR_CAP_AMBIENT_RAISE, it needs the cap
# be present in both permitted and inheritable cap set.
	sudo setcap 'cap_sys_admin=+ep cap_net_admin,cap_net_raw=+ep' bin/orchestrator

grpc-gen:
	protoc --go_out=../shared/grpc/orchestrator  --go_opt=paths=source_relative \
		--go-grpc_out=../shared/grpc/orchestrator --go-grpc_opt=paths=source_relative \
		"orchestrator.proto"

.PHONY: build-bind-mount
build-bind-mount:
	$(MAKE) -C ../shared build-bind-mount
	rm -f ./bin/bind_mount && ln -s ../../shared/bin/bind_mount ./bin/bind_mount
