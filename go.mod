module github.com/zorneth/osg-gateway

go 1.27.0

require (
	github.com/zorneth/osg-core v0.1.0-alpha.1
	github.com/zorneth/osg-providers v0.1.0-alpha.1
	github.com/zorneth/osg-runtime v0.1.0-alpha.1
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/glaciforge/slogx v0.0.0 // indirect
	github.com/lkmavi/saferefl v0.4.0 // indirect
	go.opentelemetry.io/otel v1.46.0 // indirect
	go.opentelemetry.io/otel/sdk v1.46.0 // indirect
	go.opentelemetry.io/otel/trace v1.46.0 // indirect
)

replace (
	github.com/glaciforge/slogx => ../slogx
	github.com/zorneth/osg-core => ../osg-core
	github.com/zorneth/osg-providers => ../osg-providers
	github.com/zorneth/osg-runtime => ../osg-runtime
)
