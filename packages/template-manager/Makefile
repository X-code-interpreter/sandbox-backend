.PHONY: build
build: build-bind-mount
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/template-manager .
	@echo "ask sudo to assign cap_sys_admin to template-manager"
	sudo setcap 'cap_sys_admin,cap_net_admin=+ep' bin/template-manager

.PHONY: build-bind-mount
build-bind-mount:
	$(MAKE) -C ../shared build-bind-mount
	rm -f ./bin/bind_mount && ln -s ../../shared/bin/bind_mount ./bin/bind_mount
