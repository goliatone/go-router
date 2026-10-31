#!/bin/bash
# Focused orchestration checks; no downloads or real quality-gate execution.
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
fixture_root=$(mktemp -d "${TMPDIR:-/tmp}/go-router-quality-test.XXXXXX")
fixture_root=$(cd "${fixture_root}" && pwd -P)
trap 'rm -rf "${fixture_root}"' EXIT
source "${repo_root}/taskfile"

function assert_equal {
    if [ "$1" != "$2" ]; then
        printf 'expected: %s\nactual: %s\n' "$2" "$1" >&2
        exit 1
    fi
}

function assert_status {
    local expected=$1 actual=0
    shift
    "$@" || actual=$?
    assert_equal "${actual}" "${expected}"
}

function test_identity_and_install {
    (
        local fixture="${fixture_root}/identity with spaces"
        mkdir -p "${fixture}/path" "${fixture}/installed"
        export QUALITY_TEST_INSTALL_DIR="${fixture}/installed"
        export QUALITY_TEST_INSTALL_LOG="${fixture}/install.log"
        export QUALITY_TEST_VERSION="${GO_GOHAWK_VERSION}"
        export QUALITY_TEST_INSTALL_MODE=ok
        cat > "${fixture}/go" <<'GO'
#!/bin/bash
set -eu
case "$1 $2" in
    'env GOBIN') printf '%s\n' "${QUALITY_TEST_INSTALL_DIR}" ;;
    'version -m') cat "$3.metadata" ;;
    'install '*)
        printf '%s\n' "$2" >> "${QUALITY_TEST_INSTALL_LOG}"
        [ "${QUALITY_TEST_INSTALL_MODE}" != fail ] || exit 1
        printf '#!/bin/bash\nexit 99\n' > "${QUALITY_TEST_INSTALL_DIR}/gohawk"
        chmod +x "${QUALITY_TEST_INSTALL_DIR}/gohawk"
        version=${QUALITY_TEST_VERSION}
        [ "${QUALITY_TEST_INSTALL_MODE}" != wrong ] || version=wrong
        printf 'path github.com/kojah/gohawk\nmod github.com/kojah/gohawk %s\n' "$version" > "${QUALITY_TEST_INSTALL_DIR}/gohawk.metadata"
        ;;
    *) exit 99 ;;
esac
GO
        chmod +x "${fixture}/go"
        function _go:bin { printf '%s\n' "${fixture}/go"; }
        function _go:quality:prepare_env { :; }
        export PATH="${fixture}/path:${PATH}"
        local candidate="${fixture}/path/gohawk"
        printf '#!/bin/bash\nexit 99\n' > "${candidate}"
        chmod +x "${candidate}"
        # A script/unreadable build identity is never accepted.
        assert_status 1 _go:gohawk:matches "${candidate}"
        printf 'path github.com/kojah/gohawk\nmod github.com/kojah/gohawk %s\n' "${GO_GOHAWK_VERSION}" > "${candidate}.metadata"
        assert_equal "$(_go:gohawk:ensure)" "${candidate}"
        test ! -e "${QUALITY_TEST_INSTALL_LOG}"
        printf 'path other/tool\nmod github.com/kojah/gohawk %s\n' "${GO_GOHAWK_VERSION}" > "${candidate}.metadata"
        assert_status 1 _go:gohawk:matches "${candidate}"
        printf 'path github.com/kojah/gohawk\nmod other/module %s\n' "${GO_GOHAWK_VERSION}" > "${candidate}.metadata"
        assert_status 1 _go:gohawk:matches "${candidate}"
        printf 'path github.com/kojah/gohawk\nmod github.com/kojah/gohawk wrong\n' > "${candidate}.metadata"
        assert_status 1 _go:gohawk:matches "${candidate}"
        # A stale PATH copy must not shadow the verified installation.
        assert_equal "$(_go:gohawk:ensure)" "${QUALITY_TEST_INSTALL_DIR}/gohawk"
        assert_equal "$(cat "${QUALITY_TEST_INSTALL_LOG}")" "github.com/kojah/gohawk@${GO_GOHAWK_VERSION}"
        assert_equal "$(_go:gohawk:ensure)" "${QUALITY_TEST_INSTALL_DIR}/gohawk"
        assert_equal "$(wc -l < "${QUALITY_TEST_INSTALL_LOG}" | tr -d ' ')" 1
        cp "${QUALITY_TEST_INSTALL_DIR}/gohawk.metadata" "${candidate}.metadata"
        printf '=> local/replacement (devel)\n' >> "${candidate}.metadata"
        assert_status 1 _go:gohawk:matches "${candidate}"
        # Installation errors and a successful install with wrong identity fail.
        QUALITY_TEST_INSTALL_MODE=fail
        assert_status 1 go:tools:gohawk
        QUALITY_TEST_INSTALL_MODE=wrong
        assert_status 1 go:tools:gohawk
        # Relative PATH entries stay executable after a module-directory change.
        QUALITY_TEST_INSTALL_MODE=ok
        go:tools:gohawk >/dev/null
        cp "${QUALITY_TEST_INSTALL_DIR}/gohawk.metadata" "${candidate}.metadata"
        cd "${fixture}"
        PATH="path:${PATH}"
        assert_equal "$(_go:gohawk:ensure)" "${candidate}"
    )
}

function test_module_discovery_and_scans {
    (
        local fixture="${fixture_root}/modules"
        mkdir -p "${fixture}/tools/quality" "${fixture}/evidence/frozen" "${fixture}/.ctx/archive"
        cp "${repo_root}/tools/quality/nested_modules.py" "${fixture}/tools/quality/"
        cp "${repo_root}/tools/quality/nested-go-modules.json" "${fixture}/tools/quality/"
        local modules module
        modules=$(python3 -c 'import json,sys; print("\n".join(json.load(open(sys.argv[1]))))' "${fixture}/tools/quality/nested-go-modules.json")
        for module in ${modules}; do
            mkdir -p "${fixture}/${module}"
            touch "${fixture}/${module}/go.mod"
        done
        touch "${fixture}/go.mod" "${fixture}/evidence/go.mod" "${fixture}/evidence/frozen/go.mod" "${fixture}/.ctx/archive/go.mod"
        cd "${fixture}"
        git init -q
        assert_equal "$(_go:module_dirs)" "$(printf '.\n%s' "${modules}")"
        assert_equal "$(python3 tools/quality/nested_modules.py)" "$(python3 -c 'import json; print(json.dumps(sorted(json.load(open("tools/quality/nested-go-modules.json"))),separators=(",",":")))')"
        export QUALITY_TEST_SCAN_LOG="${fixture}/scan.log"
        export QUALITY_TEST_ROOT_EXIT=3 QUALITY_TEST_NESTED_EXIT=0
        cat > "${fixture}/gohawk" <<'HAWK'
#!/bin/bash
printf '%s|%s\n' "${PWD}" "$*" >> "${QUALITY_TEST_SCAN_LOG}"
case "${PWD}" in
    */modules) exit "${QUALITY_TEST_ROOT_EXIT}" ;;
    *) exit "${QUALITY_TEST_NESTED_EXIT}" ;;
esac
HAWK
        chmod +x "${fixture}/gohawk"
        function _go:gohawk:ensure { printf '%s\n' "${fixture}/gohawk"; }
        function _go:quality:prepare_env { :; }
        local expected="${fixture}|./..."
        while IFS= read -r module; do
            expected="${expected}"$'\n'"${fixture}/${module}|./..."
        done <<< "${modules}"
        assert_status 3 go:gohawk
        assert_equal "$(cat "${QUALITY_TEST_SCAN_LOG}")" "${expected}"
        : > "${QUALITY_TEST_SCAN_LOG}"
        QUALITY_TEST_NESTED_EXIT=1
        assert_status 1 go:gohawk
        assert_equal "$(cat "${QUALITY_TEST_SCAN_LOG}")" "${expected}"
        : > "${QUALITY_TEST_SCAN_LOG}"
        QUALITY_TEST_ROOT_EXIT=1 QUALITY_TEST_NESTED_EXIT=3
        assert_status 1 go:gohawk
        assert_equal "$(cat "${QUALITY_TEST_SCAN_LOG}")" "${expected}"
        : > "${QUALITY_TEST_SCAN_LOG}"
        QUALITY_TEST_ROOT_EXIT=0 QUALITY_TEST_NESTED_EXIT=0
        go:gohawk
        assert_equal "$(cat "${QUALITY_TEST_SCAN_LOG}")" "${expected}"
        : > "${QUALITY_TEST_SCAN_LOG}"
        go:gohawk ./flash ./eventstream
        assert_equal "$(cat "${QUALITY_TEST_SCAN_LOG}")" "${fixture}|./flash ./eventstream"
        : > "${QUALITY_TEST_SCAN_LOG}"
        mkdir -p unexpected
        touch unexpected/go.mod
        assert_status 1 go:gohawk
        assert_equal "$(cat "${QUALITY_TEST_SCAN_LOG}")" ''
        rm unexpected/go.mod
        rm "${modules%%$'\n'*}/go.mod"
        assert_status 1 go:gohawk
        assert_equal "$(cat "${QUALITY_TEST_SCAN_LOG}")" ''
    )
}

function test_quality_gate {
    (
        local log="${fixture_root}/gate.log" fail_task=''
        function record { printf '%s|%s\n' "$1" "${*:2}" >> "${log}"; [ "$1" != "${fail_task}" ]; }
        function go:fmt { record fmt; }
        function go:fix { record fix "$@"; }
        function go:fmt:check { record fmt-check; }
        function go:test { record test "$@"; }
        function go:race { record race "$@"; }
        function go:lint:new { record lint-new "$@"; }
        function go:security:all { record security "$@"; }
        function go:gohawk { record gohawk "$@"; }
        local expected=$'fmt|\nfix|./flash ./eventstream\nfmt-check|\ntest|./flash ./eventstream\nrace|./flash ./eventstream\nlint-new|./flash ./eventstream\nsecurity|./flash ./eventstream\ngohawk|./flash ./eventstream'
        go:quality:all ./flash ./eventstream
        assert_equal "$(cat "${log}")" "${expected}"
        : > "${log}"
        fail_task=gohawk
        assert_status 1 go:quality:all ./flash ./eventstream
        assert_equal "$(cat "${log}")" "${expected}"
        : > "${log}"
        fail_task=race
        assert_status 1 go:quality:all ./flash ./eventstream
        assert_equal "$(tail -1 "${log}")" 'race|./flash ./eventstream'
        : > "${log}"
        fail_task=lint-new
        assert_status 1 go:quality:all ./flash ./eventstream
        assert_equal "$(tail -1 "${log}")" 'lint-new|./flash ./eventstream'
        : > "${log}"
        function go:tools:golangci-lint { record lint-install; }
        function go:tools:govulncheck { record vuln-install; }
        function go:tools:gosec { record sec-install; }
        function go:tools:gohawk { record hawk-install; }
        fail_task=''
        go:tools:all
        assert_equal "$(cat "${log}")" $'lint-install|\nvuln-install|\nsec-install|\nhawk-install|'
        : > "${log}"
        fail_task=hawk-install
        assert_status 1 go:tools:all
        assert_equal "$(cat "${log}")" $'lint-install|\nvuln-install|\nsec-install|\nhawk-install|'
    )
}

test_identity_and_install
test_module_discovery_and_scans
test_quality_gate
echo 'PASS: tool identity/install, module discovery/scans, and quality gate'
