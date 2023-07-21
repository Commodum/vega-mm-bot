module vega-mm

go 1.20

require (
	code.vegaprotocol.io/vega v0.71.6
	github.com/davecgh/go-spew v1.1.1
	github.com/gorilla/websocket v1.5.0
	github.com/shopspring/decimal v1.3.1
	golang.org/x/exp v0.0.0-20230206171751-46f607a40771
	google.golang.org/grpc v1.53.0
)

require (
	github.com/btcsuite/btcd v0.23.4 // indirect
	github.com/ethereum/go-ethereum v1.11.6 // indirect
	github.com/golang/protobuf v1.5.3 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.9.0 // indirect
	golang.org/x/crypto v0.7.0 // indirect
	golang.org/x/net v0.8.0 // indirect
	golang.org/x/sys v0.7.0 // indirect
	golang.org/x/text v0.8.0 // indirect
	google.golang.org/genproto v0.0.0-20230110181048-76db0878b65f // indirect
	google.golang.org/protobuf v1.30.0 // indirect
)

replace github.com/btcsuite/btcd => github.com/btcsuite/btcd v0.22.3
