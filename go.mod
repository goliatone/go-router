module github.com/goliatone/go-router

go 1.26.0

require (
	dario.cat/mergo v1.0.2
	github.com/ettle/strcase v0.2.0
	github.com/flosch/pongo2/v6 v6.1.0
	github.com/gertd/go-pluralize v0.2.1
	github.com/getkin/kin-openapi v0.133.0
	github.com/gobwas/glob v0.2.3
	github.com/gofiber/contrib/websocket v1.3.4
	github.com/gofiber/fiber/v2 v2.52.13
	github.com/gofiber/template v1.8.3
	github.com/gofiber/template/django/v3 v3.1.14
	github.com/gofiber/utils v1.2.0
	github.com/goliatone/go-command v0.24.3
	github.com/goliatone/go-composite-fs v0.3.0
	github.com/goliatone/go-errors v0.12.0
	github.com/goliatone/go-featuregate v0.6.1
	github.com/goliatone/hashid v0.2.3
	github.com/goodsign/monday v1.0.2
	github.com/google/uuid v1.6.0
	// Pinned to untagged commit d67f418 ("Use crypto/rand for mask key") on purpose.
	// GO-2026-6278 lists v1.5.3 as fixed, but v1.5.3 still generates mask keys with
	// math/rand; the crypto/rand fix landed on main after v1.5.3 and is not tagged.
	// Don't `go get github.com/gorilla/websocket@latest`: it resolves to v1.5.3 and
	// downgrades. Move to the first tagged release that includes d67f418.
	// Unlike v1.5.3, this picks subprotocols in client order and answers 426 when
	// the Upgrade header lacks "websocket".
	github.com/gorilla/websocket v1.5.4-0.20240701034025-d67f41855da4
	github.com/julienschmidt/httprouter v1.3.0
	github.com/stretchr/testify v1.12.1
	github.com/valyala/fasthttp v1.76.0
	golang.org/x/time v0.14.0
	gopkg.in/yaml.v2 v2.4.0
)

require (
	github.com/goliatone/go-auth v0.45.4
	github.com/goliatone/go-crud v0.26.1
	github.com/goliatone/go-repository-bun v0.16.2
	github.com/goliatone/go-users v0.27.3
)

require (
	github.com/gofiber/schema v1.9.0 // indirect
	github.com/gofiber/utils/v2 v2.6.2 // indirect
	github.com/molecule-man/go-brrr v1.3.0 // indirect
	github.com/philhofer/fwd v1.2.1 // indirect
	github.com/tinylib/msgp v1.6.5 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
)

require (
	github.com/MicahParks/keyfunc/v2 v2.1.0 // indirect
	github.com/alecthomas/chroma/v2 v2.20.0 // indirect
	github.com/alecthomas/kong v1.13.0 // indirect
	github.com/andybalholm/brotli v1.2.1 // indirect
	github.com/asaskevich/govalidator v0.0.0-20230301143203-a9d515a09cc2 // indirect
	github.com/clipperhouse/stringish v0.1.1 // indirect
	github.com/clipperhouse/uax29/v2 v2.3.0 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/dlclark/regexp2 v1.11.5 // indirect
	github.com/fasthttp/websocket v1.5.12 // indirect
	github.com/fatih/color v1.18.0 // indirect
	github.com/flosch/pongo2/v7 v7.0.0-alpha.1.0.20260313142248-c0f82578df57
	github.com/go-openapi/jsonpointer v0.21.0 // indirect
	github.com/go-openapi/swag v0.23.0 // indirect
	github.com/go-ozzo/ozzo-validation/v4 v4.3.0 // indirect
	github.com/gofiber/contrib/v3/websocket v1.2.9
	github.com/gofiber/fiber/v3 v3.6.1
	github.com/gofiber/template/django/v4 v4.0.12
	github.com/gofiber/template/v2 v2.1.4
	github.com/golang-jwt/jwt/v5 v5.3.0 // indirect
	github.com/goliatone/go-i18n v0.5.0 // indirect
	github.com/goliatone/go-logger v0.10.1 // indirect
	github.com/goliatone/go-masker v0.2.0 // indirect
	github.com/goliatone/go-print v0.4.1 // indirect
	github.com/goliatone/go-slug v0.1.0 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/klauspost/compress v1.20.3 // indirect
	github.com/lib/pq v1.10.9 // indirect
	github.com/lithammer/shortuuid v3.0.0+incompatible // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/mattn/go-colorable v0.1.16 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/mattn/go-runewidth v0.0.19 // indirect
	github.com/mattn/go-sqlite3 v1.14.34 // indirect
	github.com/mohae/deepcopy v0.0.0-20170929034955-c48cc78d4826 // indirect
	github.com/oasdiff/yaml v0.0.0-20250309154309-f31be36b4037 // indirect
	github.com/oasdiff/yaml3 v0.0.0-20250309153720-d2182401db90 // indirect
	github.com/perimeterx/marshmallow v1.1.5 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/puzpuzpuz/xsync/v3 v3.5.1 // indirect
	github.com/savsgio/gotils v0.0.0-20250924091648-bce9a52d7761 // indirect
	github.com/showa-93/go-mask v0.6.2 // indirect
	github.com/stretchr/objx v0.8.0 // indirect
	github.com/tmthrgd/go-hex v0.0.0-20190904060850-447a3041c3bc // indirect
	github.com/uptrace/bun v1.2.18 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/vmihailenco/msgpack/v5 v5.4.1 // indirect
	github.com/vmihailenco/tagparser/v2 v2.0.0 // indirect
	github.com/woodsbury/decimal128 v1.3.0 // indirect
	golang.org/x/crypto v0.58.0 // indirect
	golang.org/x/net v0.61.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/text v0.43.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
