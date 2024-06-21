.PHONY: build
build-ci:
	# ci means code interpreter here
	docker build -t jialianghuang/default-code-interpreter:latest \
		--network host -f ci.Dockerfile .
		# for network proxy
		# --build-arg HTTP_PROXY=http://127.0.0.1:7890 \
		# --build-arg HTTPS_PROXY=http://127.0.0.1:7890

build-metagpt:
	docker build -t jialianghuang/metagpt-sandbox:latest \
		--network host  -f metagpt.Dockerfile .
