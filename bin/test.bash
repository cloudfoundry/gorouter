#!/bin/bash

set -eu
set -o pipefail

configure_rsyslog
configure_db "${DB}"
# shellcheck disable=SC2068
# Double-quoting array expansion here causes ginkgo to fail
go run github.com/onsi/ginkgo/v2/ginkgo ${@}
