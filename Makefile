# Copyright 2018 Intel Corporation
#
# SPDX-License-Identifier: GPL-3.0-only

.NOTPARALLEL:

top_srcdir = $(abspath .)
MAKEFLAGS += -r --no-print-directory

build_dir = $(top_srcdir)/build
build_bin_dir = $(build_dir)/bin
pkg_dir = $(top_srcdir)
cov_dir = $(top_srcdir)/.coverage

orig_go_path = $(shell go env GOPATH)
export GOPATH=$(pkg_dir)
export GO_PACKAGE_PREFIX := github.com/clearlinux/clr-installer
export TESTS_DIR := $(top_srcdir)/tests/
export TRAVIS_CONF = $(top_srcdir)/.travis.yml
export UPDATE_COVERAGE = 1

CLR_INSTALLER_TEST_HTTP_PORT ?= 8181

export TEST_HTTP_PORT = ${CLR_INSTALLER_TEST_HTTP_PORT}

THEME_DIR=$(DESTDIR)/usr/share/clr-installer/themes/
DESKTOP_DIR=$(DESTDIR)/usr/share/applications/
CONFIG_DIR=$(DESTDIR)/usr/share/defaults/clr-installer/
SYSTEMD_DIR=$(DESTDIR)/usr/lib/systemd/system/

BUILDDATE=$(shell date -u "+%Y-%m-%d_%H:%M:%S_%Z")
# Are we running from a Git Repo?
$(shell [ -d .git ] || git rev-parse --is-inside-work-tree > /dev/null 2>&1)
ifeq ($(.SHELLSTATUS),0)
IS_GIT_REPO=1
else
IS_GIT_REPO=0
endif

ifeq ($(IS_GIT_REPO),1)
# Use the git tag and SHA
# Standard build case from Git repo
VERSION=$(shell git describe --tags --always --dirty  --match '[0-9]*.[0-9]*.[0-9]*' --exclude '[0-9]*.[0-9]*.[0-9]*.*[0-9]')
else
# If VERSION is defined in the environment, use it; otherwise...
ifeq ($(VERSION),)
# Attempt to parse from the directory name
# Building from a versioned source archive
mkfile_dir=$(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
VERSION=$(shell basename $(mkfile_dir) | awk -F- '{print $$NF}')
endif
endif

# Validate the Version
validate_version:
ifeq ($(IS_GIT_REPO),0)
ifeq (,$(shell echo $(VERSION) | egrep '^[0-9]+.[0-9]+.[0-9]+$$' 2> /dev/null))
	@echo "Invalid version string: $(VERSION)"
	@exit 1
endif
endif

.PHONY: gopath
ifeq (,$(findstring ${GO_PACKAGE_PREFIX},${CURDIR}))
LOCAL_GOPATH := ${CURDIR}/.gopath
export GOPATH := ${LOCAL_GOPATH}
gopath:
	@rm -rf ${LOCAL_GOPATH}/src
	@mkdir -p ${LOCAL_GOPATH}/src/${GO_PACKAGE_PREFIX}
ifeq ($(IS_GIT_REPO),1)
# Smart copy only files under version control
	@tar cf - `git ls-files` | tar xf - --directory=${LOCAL_GOPATH}/src/${GO_PACKAGE_PREFIX}
else
	@cp -af * ${LOCAL_GOPATH}/src/${GO_PACKAGE_PREFIX}
endif
else
LOCAL_GOPATH :=
GOPATH ?= ${HOME}/go
gopath:
	@echo "GRS: $(GRS)"
	@echo "Code already in existing GOPATH=${GOPATH}"
endif

install: build
	@install -D -m 755 $(top_srcdir)/.gopath/bin/clr-installer $(DESTDIR)/usr/bin/clr-installer
	@install -D -m 755 $(top_srcdir)/scripts/clr-installer-desktop.sh $(DESTDIR)/usr/bin/clr-installer-desktop.sh
	@install -D -m 644  $(top_srcdir)/themes/clr-installer.theme $(THEME_DIR)/clr-installer.theme
	@install -D -m 644  $(top_srcdir)/etc/clr-installer.yaml $(CONFIG_DIR)/clr-installer.yaml
	@install -D -m 644  $(top_srcdir)/etc/bundles.json $(CONFIG_DIR)/bundles.json
	@install -D -m 644  $(top_srcdir)/etc/kernels.json $(CONFIG_DIR)/kernels.json
	@install -D -m 644  $(top_srcdir)/etc/clr-installer.desktop $(DESKTOP_DIR)/clr-installer.desktop
	@install -D -m 644 $(top_srcdir)/etc/systemd/clr-installer.service $(SYSTEMD_DIR)/clr-installer.service
	@install -D -m 644  $(top_srcdir)/etc/chpasswd $(CONFIG_DIR)/chpasswd

uninstall:
	@rm -f $(DESTDIR)/usr/bin/clr-installer
	@rm -f $(DESTDIR)/usr/bin/clr-installer-desktop.sh
	@rm -f $(THEME_DIR)/clr-installer.theme
	@rm -f $(CONFIG_DIR)/clr-installer.yaml
	@rm -f $(CONFIG_DIR)/bundles.json
	@rm -f $(CONFIG_DIR)/kernels.json
	@rm -f $(DESKTOP_DIR)/clr-installer.desktop
	@rm -f $(SYSTEMD_DIR)/clr-installer.service
	@rm -f $(CONFIG_DIR)/chpasswd
	@rm -f $(DESTDIR)/var/lib/clr-installer/clr-installer.yaml

build-pkgs: build
	@for pkg in `find -path ./vendor -prune -o -path ./.gopath -prune -o -name "*.go" \
	   -printf "%h\n" | sort -u | sed 's/\.\///g'`; do \
	   go install -v $${GO_PACKAGE_PREFIX}/$$pkg; \
   done

build-vendor: build
	@cp -a vendor/* .gopath/src/
	@for pkg in `find ./vendor -name "*.go" \
	   -printf "%h\n" | sort -u | sed 's/\.\/vendor\///g'`; do \
	   go install -v $$pkg; \
   done
	@rm -rf .gopath/src/*

build: validate_version gopath
	go get -v ${GO_PACKAGE_PREFIX}/clr-installer
	go install -v \
		-ldflags="-X github.com/clearlinux/clr-installer/model.Version=${VERSION} \
		-X github.com/clearlinux/clr-installer/model.BuildDate=${BUILDDATE}" \
		${GO_PACKAGE_PREFIX}/clr-installer

build-local-travis: validate_version gopath
	@go get -v ${GO_PACKAGE_PREFIX}/local-travis
	@go install -v \
		-ldflags="-X github.com/clearlinux/clr-installer/model.Version=${VERSION} \
		-X github.com/clearlinux/clr-installer/model.BuildDate=${BUILDDATE}" \
		${GO_PACKAGE_PREFIX}/local-travis

check-coverage: build-local-travis
	@echo "local-travis simulation:"
	@$(top_srcdir)/.gopath/bin/local-travis

check: gopath
	@# Ensure no temp files are left behind
	@LSCMD='ls -lart --ignore="." /tmp'; \
	SHACMD='ls -art --ignore="." /tmp | sha512sum'; \
	BEFORELS=`eval $$LSCMD`; \
	BEFORESHA=`eval $$SHACMD`; \
	go test -cover ${GO_PACKAGE_PREFIX}/...; \
	AFTERSHA=`eval $$SHACMD`; \
	AFTERLS=`eval $$LSCMD`; \
	if [ "$$BEFORESHA" != "$$AFTERSHA" ] ; then \
		echo "Test Failed: Temporary directory may not be clean!"; \
		echo "Left-over files:"; \
		echo "$$BEFORELS" > /tmp/beforels; \
		echo "$$AFTERLS" > /tmp/afterls; \
		diff -Nr /tmp/beforels /tmp/afterls; \
		/bin/false ; \
	fi; \

check-clean: gopath
	go clean -testcache

check-root: gopath
	sudo -E go test -cover ${GO_PACKAGE_PREFIX}/...

PHONY += coverage
coverage: build
	@rm -rf ${cov_dir}; \
	mkdir -p ${cov_dir}; \
	for pkg in $$(go list $$GO_PACKAGE_PREFIX/...); do \
		file="${cov_dir}/$$(echo $$pkg | tr / -).cover"; \
		go test -covermode="count" -coverprofile="$$file" "$$pkg"; \
	done; \
	echo "mode: count" > ${cov_dir}/cover.out; \
	grep -h -v "^mode:" ${cov_dir}/*.cover >>"${cov_dir}/cover.out"; \

PHONY += coverage-func
coverage-func: coverage
	@go tool cover -func="${cov_dir}/cover.out"

PHONY += coverage-html
coverage-html: coverage
	@go tool cover -html="${cov_dir}/cover.out"

PHONY += install-linters
install-linters:
	@if ! gometalinter.v2 --version &>/dev/null; then \
		echo "Installing linters..."; \
		GOPATH=${orig_go_path} go get -u gopkg.in/alecthomas/gometalinter.v2; \
		GOPATH=${orig_go_path} gometalinter.v2 --install; \
	fi \

PHONY += install-linters-force
install-linters-force:
	echo "Force Installing linters..."
	GOPATH=${orig_go_path} go get -u gopkg.in/alecthomas/gometalinter.v2
	GOPATH=${orig_go_path} gometalinter.v2 --install

PHONY += update-linters
update-linters:
	@if gometalinter.v2 --version &>/dev/null; then \
		echo "Updating linters..."; \
		GOPATH=${orig_go_path} gometalinter.v2 --update 1>/dev/null; \
	else \
		echo "Linters not installed"; \
		exit 1; \
	fi \

PHONY += lint
lint: build install-linters gopath
	@echo "Running linters"
	@rm -rf ${LOCAL_GOPATH}/src/${GO_PACKAGE_PREFIX}/vendor
	@cp -af vendor/* ${LOCAL_GOPATH}/src/
	@go build -race github.com/clearlinux/clr-installer/...
	@gometalinter.v2 --deadline=10m --tests --vendor \
	--exclude=vendor --disable-all \
	--enable=misspell \
	--enable=vet \
	--enable=ineffassign \
	--enable=gofmt \
	$${CYCLO_MAX:+--enable=gocyclo --cyclo-over=$${CYCLO_MAX}} \
	--enable=golint \
	--enable=deadcode \
	--enable=varcheck \
	--enable=structcheck \
	--enable=unused \
	--enable=vetshadow \
	--enable=errcheck \
	./...

PHONY += dep-install
dep-install:
	@if ! dep version &>/dev/null; then \
		echo "Installing dep..."; \
		mkdir -p ${orig_go_path}/bin; \
		curl https://raw.githubusercontent.com/golang/dep/master/install.sh 2>/dev/null \
		| GOPATH=${orig_go_path} bash; \
	fi \

PHONY += dep-check
dep-check: dep-install
	@cd ${LOCAL_GOPATH}/src/${GO_PACKAGE_PREFIX} ; GOPATH=${LOCAL_GOPATH} dep check

PHONY += dep-update
dep-update: dep-install
	@if dep version &>/dev/null; then \
		echo "Updating dep..."; \
		curl https://raw.githubusercontent.com/golang/dep/master/install.sh 2>/dev/null \
		| GOPATH=${orig_go_path} bash; \
	else \
		echo "Dep not installed"; \
		exit 1; \
	fi \

PHONY += vendor-init
vendor-init: gopath dep-install
	@rm -rf ${LOCAL_GOPATH}/src/${GO_PACKAGE_PREFIX}/vendor
	@rm -f ${LOCAL_GOPATH}/src/${GO_PACKAGE_PREFIX}/Gopkg.*
	@cd ${LOCAL_GOPATH}/src/${GO_PACKAGE_PREFIX} ; GOPATH=${LOCAL_GOPATH} dep init
	@cp -a ${LOCAL_GOPATH}/src/${GO_PACKAGE_PREFIX}/vendor ${top_srcdir}
	@cp -a ${LOCAL_GOPATH}/src/${GO_PACKAGE_PREFIX}/Gopkg.* ${top_srcdir}

PHONY += vendor-status
vendor-status: dep-install
	@cd ${LOCAL_GOPATH}/src/${GO_PACKAGE_PREFIX} ; GOPATH=${LOCAL_GOPATH} dep status

PHONY += vendor-check
vendor-check: dep-check

PHONY += vendor-update
vendor-update: dep-install
	@# Copy the updated files from revision control area
	@cp -a ${top_srcdir}/Gopkg.* ${LOCAL_GOPATH}/src/${GO_PACKAGE_PREFIX}
	@# Pull updates
	@cd ${LOCAL_GOPATH}/src/${GO_PACKAGE_PREFIX} ; GOPATH=${LOCAL_GOPATH} dep ensure -update
	@# Copy results back to revision control area
	@cp -a ${LOCAL_GOPATH}/src/${GO_PACKAGE_PREFIX}/vendor ${top_srcdir}
	@cp -a ${LOCAL_GOPATH}/src/${GO_PACKAGE_PREFIX}/Gopkg.* ${top_srcdir}

PHONY += vendor-add
vendor-add: dep-install
	@# Copy the updated files from revision control area
	@cp -a ${top_srcdir}/Gopkg.* ${LOCAL_GOPATH}/src/${GO_PACKAGE_PREFIX}
	@# Pull updates
	@cd ${LOCAL_GOPATH}/src/${GO_PACKAGE_PREFIX} ; GOPATH=${LOCAL_GOPATH} dep ensure -add ${GOADD}
	@# Copy results back to revision control area
	@cp -a ${LOCAL_GOPATH}/src/${GO_PACKAGE_PREFIX}/vendor ${top_srcdir}
	@cp -a ${LOCAL_GOPATH}/src/${GO_PACKAGE_PREFIX}/Gopkg.* ${top_srcdir}


PHONY += tag
ifeq ($(IS_GIT_REPO),1)
tag:
	@if git diff-index --quiet HEAD &>/dev/null; then \
		if git diff @{upstream}.. --quiet &>/dev/null; then \
			echo "Create and push the Tag to GitHub"; \
			echo "git tag <version>"; \
			echo "git push <remote> <version>"; \
		else \
			echo "Unpushed changes; git push upstream and try again."; \
			exit 1; \
		fi \
	else \
		echo "Uncomiited changes; git commit and try again."; \
		exit 1; \
	fi
else
tag:
	@echo "Not running from Git Repo; tag will not work."
	@exit 1
endif

PHONY += clean
ifeq ($(IS_GIT_REPO),1)
clean:
	@go clean -i -r
	@git clean -fdXq
else
clean:
	@go clean -i -r
endif

PHONY += distclean
ifeq ($(IS_GIT_REPO),1)
dist-clean: clean
	@if [ "$$(git status -s)" = "" ]; then \
		git clean -fdxq; \
		git reset HEAD; \
		go clean -testcache; \
		go clean -modcache; \
	else \
		echo "There are pending changes in the repository!"; \
		git status -s; \
		echo "Please check in changes or stash, and try again."; \
	fi
else
dist-clean: clean
	@go clean -testcache
	@go clean -modcache
endif

all: build

PHONY += all

.PHONY = $(PHONY)
.DEFAULT_GOAL = all
