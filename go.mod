module webrtc_poc_go

go 1.23.3

require (
	github.com/bluenviron/gortsplib/v4 v4.13.1
	github.com/bluenviron/mediacommon v1.14.0
	github.com/gookit/color v1.5.4
	github.com/pion/ice/v2 v2.3.37
	github.com/pion/ion-avp v1.8.4
	github.com/pion/ion-log v1.2.2
	github.com/pion/mediadevices v0.7.1
	github.com/pion/rtp v1.8.15
	github.com/pion/webrtc/v3 v3.3.5
	github.com/pion/webrtc/v4 v4.1.0
	github.com/stretchr/testify v1.10.0
	github.com/xlab/libvpx-go v0.0.0-20220203233824-652b2616315c
	golang.org/x/exp v0.0.0-20250408133849-7e4ce0ab07d0
	golang.org/x/image v0.26.0
	golang.org/x/term v0.31.0
)

require (
	github.com/bluenviron/mediacommon/v2 v2.1.0 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mgutz/ansi v0.0.0-20200706080929-d51e80ef957d // indirect
	github.com/pion/dtls/v2 v2.2.12 // indirect
	github.com/pion/mdns v0.0.12 // indirect
	github.com/pion/srtp/v2 v2.0.20 // indirect
	github.com/pion/stun v0.6.1 // indirect
	github.com/pion/transport/v2 v2.2.10 // indirect
	github.com/pion/turn/v2 v2.1.6 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/sirupsen/logrus v1.9.3 // indirect
	github.com/xo/terminfo v0.0.0-20210125001918-ca9a967f8778 // indirect
	golang.org/x/net v0.39.0 // indirect
	golang.org/x/text v0.24.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250425173222-7b384671a197 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

require (
	github.com/google/uuid v1.6.0
	github.com/gorilla/websocket v1.5.3
	github.com/pion/datachannel v1.5.10 // indirect
	github.com/pion/dtls/v3 v3.0.6 // indirect
	github.com/pion/ice/v4 v4.0.10 // indirect
	github.com/pion/interceptor v0.1.37
	github.com/pion/logging v0.2.3 // indirect
	github.com/pion/mdns/v2 v2.0.7 // indirect
	github.com/pion/randutil v0.1.0 // indirect
	github.com/pion/rtcp v1.2.15
	github.com/pion/sctp v1.8.39 // indirect
	github.com/pion/sdp/v3 v3.0.11
	github.com/pion/srtp/v3 v3.0.4 // indirect
	github.com/pion/stun/v3 v3.0.0 // indirect
	github.com/pion/transport/v3 v3.0.7 // indirect
	github.com/pion/turn/v4 v4.0.1 // indirect
	github.com/wlynxg/anet v0.0.5 // indirect
	golang.org/x/crypto v0.37.0 // indirect
	golang.org/x/sys v0.32.0 // indirect
	google.golang.org/grpc v1.72.0
	google.golang.org/protobuf v1.36.6
)

replace github.com/pion/ice/v2 => github.com/aler9/ice/v2 v2.0.0-20240608212222-2eebc68350c9

replace github.com/pion/webrtc/v3 => github.com/aler9/webrtc/v3 v3.0.0-20240610104456-eaec24056d06
