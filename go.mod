module github.com/zorneth/osg-gateway

go 1.27.0

require (
	github.com/zorneth/osg-core v0.0.0
	github.com/zorneth/osg-runtime v0.0.0
	gopkg.in/yaml.v3 v3.0.1
)

replace github.com/zorneth/osg-core => ../osg-core

replace github.com/zorneth/osg-runtime => ../osg-runtime
