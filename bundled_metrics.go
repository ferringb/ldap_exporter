package main

//go:generate go run assets_generate.go

import (
	"github.com/prometheus/common/log"
	"gopkg.in/ldap.v2"
	"io/ioutil"
)

func loadBundledMetricsForServer(conn *ldap.Conn) ([]*MetricsSource, error) {
	// The intent here is to identify the server- if we can- and load any
	// bundled metrics we know of for that server.
	log.Debug("attempting to identify the ldap vendor for the given service...")
	sr, err := conn.Search(
		ldap.NewSearchRequest(
			"",
			ldap.ScopeBaseObject, ldap.NeverDerefAliases, 0, 0, false,
			"(objectClass=*)",
			[]string{"vendorname"},
			nil,
		),
	)
	// if we couldn't even search, return the error.
	if err != nil {
		return nil, err
	}
	// for 389, it would be something like thus for example:
	// dn:
	// vendorname: 389 Project
	// vendorversion: 389-Directory/1.3.5.18 B2017.193.1637

	for _, entry := range sr.Entries {
		for _, ea := range entry.Attributes {
			if ea.Name == "vendorname" && len(ea.Values) == 1 && ea.Values[0] == "389 Project" {
				log.Info("Loading bundled metrics for LDAP vendor 389 directory")
				return loadBundledConfig("definitions/389.yaml")
			}
		}
	}
	log.Warn("Couldn't identify the LDAP vendor, no bundled metrics will be enabled")
	return []*MetricsSource{}, nil
}

func loadBundledConfig(asset_name string) ([]*MetricsSource, error) {
	data, err := assets.Open(asset_name)
	if err != nil {
		return nil, err
	}
	content, err := ioutil.ReadAll(data)
	if err != nil {
		return nil, err
	}
	return LoadConfig(string(content))
}
