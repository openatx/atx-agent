package main

import (
	"time"

	"github.com/openatx/androidutils"
)

type CpuInfo struct {
	Cores    int    `json:"cores"`
	Hardware string `json:"hardware"`
}

type MemoryInfo struct {
	Total  int    `json:"total"` // unit kB
	Around string `json:"around,omitempty"`
}

type OwnerInfo struct {
	IP string `json:"ip"`
}

type DeviceInfo struct {
	Udid                   string                `json:"udid,omitempty"` // Unique device identifier
	AndroidId              string                `json:"android_id,omitempty"`
	PropertyId             string                `json:"propertyId,omitempty"` // For device managerment, eg: HIH-PHO-1122
	Version                string                `json:"version,omitempty"`    // ro.build.version.release
	Serial                 string                `json:"serial,omitempty"`     // ro.serialno
	Brand                  string                `json:"brand,omitempty"`      // ro.product.brand
	Model                  string                `json:"model,omitempty"`      // ro.product.model
	HWAddr                 string                `json:"hwaddr,omitempty"`     // persist.sys.wifi.mac
	Notes                  string                `json:"notes,omitempty"`      // device notes
	IP                     string                `json:"ip,omitempty"`
	Port                   int                   `json:"port,omitempty"`
	ReverseProxyAddr       string                `json:"reverseProxyAddr,omitempty"`
	ReverseProxyServerAddr string                `json:"reverseProxyServerAddr,omitempty"`
	Sdk                    int                   `json:"sdk,omitempty"`
	AgentVersion           string                `json:"agentVersion,omitempty"`
	Display                *androidutils.Display `json:"display,omitempty"`
	Battery                *androidutils.Battery `json:"battery,omitempty"`
	Memory                 *MemoryInfo           `json:"memory,omitempty"` // proc/meminfo
	Cpu                    *CpuInfo              `json:"cpu,omitempty"`    // proc/cpuinfo
	Arch                   string                `json:"arch"`

	Owner    *OwnerInfo `json:"owner" gorethink:"owner,omitempty"`
	Reserved string     `json:"reserved,omitempty"`

	ConnectionCount   int       `json:"-"` // > 1 happended when phone redial server
	CreatedAt         time.Time `json:"-" gorethink:"createdAt,omitempty"`
	PresenceChangedAt time.Time `json:"presenceChangedAt,omitempty"`
	UsingBeganAt      time.Time `json:"usingBeganAt,omitempty" gorethink:"usingBeganAt,omitempty"`

	Ready   *bool `json:"ready,omitempty"`
	Present *bool `json:"present,omitempty"`
	Using   *bool `json:"using,omitempty"`

	Product  *Product  `json:"product" gorethink:"product_id,reference,omitempty" gorethink_ref:"id"`
	Provider *Provider `json:"provider" gorethink:"provider_id,reference,omitempty" gorethink_ref:"id"`

	// only works when there is provider
	ProviderForwardedPort int `json:"providerForwardedPort,omitempty"`

	// used for provider to known agent server url
	ServerURL string `json:"serverUrl,omitempty"`
}

// "Brand Model Memory CPU" together can define a phone
type Product struct {
	Id     string `json:"id" gorethink:"id,omitempty"`
	Name   string `json:"name" gorethink:"name,omitempty"`
	Brand  string `json:"brand" gorethink:"brand,omitempty"`
	Model  string `json:"model" gorethink:"model,omitempty"`
	Memory string `json:"memory,omitempty"` // eg: 4GB
	Cpu    string `json:"cpu,omitempty"`

	Coverage float32 `json:"coverage" gorethink:"coverage,omitempty"`
	Gpu      string  `json:"gpu,omitempty"`
	Link     string  `json:"link,omitempty"` // Outside link
	// AntutuScore int     `json:"antutuScore,omitempty"`
}

// u2init
type Provider struct {
	Id                string    `json:"id" gorethink:"id,omitempty"` // machine id
	Local             string    `json:"local,omitempty"`
	Remote            string    `json:"remote,omitempty"`
	Present           *bool     `json:"present,omitempty"`
	Notes             string    `json:"notes" gorethink:"notes,omitempty"`
	CreatedAt         time.Time `json:"createdAt,omitempty"`
	PresenceChangedAt time.Time `json:"presenceChangedAt,omitempty"`
}

func (p *Provider) Addr() string {
	return p.Local
}

func (p *Provider) RemoteAddr() string {
	return p.Remote
}
