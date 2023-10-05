module vega-mm

go 1.20

require (
	code.vegaprotocol.io/quant v0.2.5
	code.vegaprotocol.io/vega v0.71.6
	github.com/jeremyletang/vega-go-sdk v0.0.0-20230123175705-c0e54d7c02f5
	github.com/prometheus/client_golang v1.14.0
	github.com/shopspring/decimal v1.3.1
	golang.org/x/exp v0.0.0-20230321023759-10a507213a29
	google.golang.org/grpc v1.53.0
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/ethereum/go-ethereum v1.11.6 // indirect
	github.com/golang/protobuf v1.5.3 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.9.0 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.4 // indirect
	github.com/prometheus/client_model v0.3.0 // indirect
	github.com/prometheus/common v0.42.0 // indirect
	github.com/prometheus/procfs v0.9.0 // indirect
	golang.org/x/crypto v0.7.0 // indirect
	golang.org/x/net v0.8.0 // indirect
	golang.org/x/sys v0.7.0 // indirect
	golang.org/x/text v0.8.0 // indirect
	gonum.org/v1/gonum v0.12.0 // indirect
	google.golang.org/genproto v0.0.0-20230110181048-76db0878b65f // indirect
	google.golang.org/protobuf v1.30.0 // indirect
)

replace (
	code.vegaprotocol.io/vega => ../vega
	github.com/btcsuite/btcd => github.com/btcsuite/btcd v0.22.3
)
