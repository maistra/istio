package features

import (
	"strings"

	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/pkg/env"
	"istio.io/pkg/log"

	"sync"
)

var (
	ALPNProtocols = RegisterALPNProtocolsVar(
		"ALPN_PROTOCOLS",
		"",
		"The supported ALPN Protocols",
	)
)

type ALPNProtocolsVar struct {
	env.StringVar
	protocols        []string
	alpnProtocolLock *sync.Mutex
}

func RegisterALPNProtocolsVar(name string, defaultValue string, description string) ALPNProtocolsVar {
	v := env.RegisterStringVar(name, defaultValue, description)
	return ALPNProtocolsVar{v, nil, &sync.Mutex{}}
}

func (v *ALPNProtocolsVar) initAlpnProtocols() {
	v.alpnProtocolLock.Lock()
	defer v.alpnProtocolLock.Unlock()
	if v.protocols == nil {
		protocols := []string{}
		protocolsParam, _ := v.Lookup()
		if protocolsParam != "" {
			protocolsParamSlice := strings.Split(protocolsParam, ",")
			for _, protocolsParam := range protocolsParamSlice {
				trimmed := strings.Trim(protocolsParam, " ")
				// ensure only supported values are accepted
				if trimmed == util.ALPNH11Only[0] || trimmed == util.ALPNH2Only[0] {
					protocols = append(protocols, trimmed)
				} else {
					log.Warnf("ALPN Protocol %v is not supported, this entry will be ignored", trimmed)
				}
			}
		}
		if len(protocols) == 0 {
			v.protocols = util.ALPNHttp
		} else {
			v.protocols = protocols
		}
		log.Infof("ALPN Protocol is %v", v.protocols)
	}
}

func (v *ALPNProtocolsVar) Reset() {
	v.alpnProtocolLock.Lock()
	defer v.alpnProtocolLock.Unlock()
	v.protocols = nil
}

func (v *ALPNProtocolsVar) Get() []string {
	if v.protocols == nil {
		v.initAlpnProtocols()
	}
	return v.protocols
}
