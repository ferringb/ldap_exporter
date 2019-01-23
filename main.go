package main

import (
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/log"
	"gopkg.in/ldap.v2"
)

var (
	listen              = flag.String("web.listen-address", ":9095", "The host:port to listen on for HTTP requests")
	metricsPath         = flag.String("web.telemetry-path", "/metrics", "Path under which to expose metrics")
	ldap_uri            = flag.String("ldap.uri", "", "Openldap compatible URI to connect to.  Can use ldap://, ldaps://, ldapi://")
	ldap_tls_ca         = flag.String("ldap.tls.ca-file", "", "If TLS is used, the path for to CA to use")
	ldap_tls_cert       = flag.String("ldap.tls.cert-file", "", "If the server requires a client cert, the path to that TLS cert.  If this is passed, -ldap.tls.key-file must also be passed")
	ldap_tls_key        = flag.String("ldap.tls.key-file", "", "If the server requires a client key, the path to that TLS key.  If this is passed, -ldap.tls.cert-file must also be passed")
	ldap_tls_serverName = flag.String("ldap.tls.server-name", "", "If specified, expect this name for TLS handshakes rather than using the hostname parsed from -ldap.uri")
	ldap_tls_skipVerify = flag.Bool("ldap.tls.skip-verify", false, "If given, do not do any verification of the server's cert.  Insecure and allows for MITM")
	ldap_bind           = flag.String("ldap.bind", "", "Ldap DN to bind to")
	ldap_password       = flag.String("ldap.password", os.Getenv("LDAP_PASSWORD"), "LDAP bind DN password.  Can be configured via the environment variable LDAP_PASSWORD")

	disableVendorMetrics = flag.Bool("metrics.disable-vendor-metrics", false, "By default, try to identify the LDAP vendor and load metrics for thhat vendor.  If the vendor cannot be identified or if this is enabled,, -metrics.config must be set.")
	queryFile            = flag.String("metrics.config", "", "YAML file holding ldap -> metrics queries.  Note if the LDAP vendor cannot be identified, this must be set")
)

func createTLSConfigFromFlags() (*tls.Config, error) {
	var ca_pool *x509.CertPool
	var certs []tls.Certificate

	if *ldap_tls_ca != "" {
		ca_content, err := ioutil.ReadFile(*ldap_tls_ca)
		if err != nil {
			return nil, err
		}
		ca_pool = x509.NewCertPool()
		if !ca_pool.AppendCertsFromPEM(ca_content) {
			return nil, fmt.Errorf("failed to read ca_file %v in PEM format", *ldap_tls_ca)
		}
	}

	if *ldap_tls_cert != "" {
		if *ldap_tls_key == "" {
			return nil, fmt.Errorf("passed -ldap.tls.cert-file but required -ldap.tls.key-file wasn't passed")
		}
		cert, err := tls.LoadX509KeyPair(*ldap_tls_cert, *ldap_tls_key)
		if err != nil {
			return nil, err
		}
		certs = append(certs, cert)
	} else if *ldap_tls_key != "" {
		return nil, fmt.Errorf("passed -ldap.tls.key-file but required -ldap.tls.cert-file wasn't passed")
	}
	config := &tls.Config{
		InsecureSkipVerify: *ldap_tls_skipVerify,
		RootCAs:            ca_pool,
		Certificates:       certs,
	}
	return config, nil
}

func createLdapClientFromFlags(ldap_uri string, serverName string, tls_config *tls.Config) (*ldap.Conn, error) {
	if ldap_uri == "" {
		return nil, fmt.Errorf("-ldap.uri is a required argument")
	}
	u, err := url.Parse(ldap_uri)
	if err != nil {
		return nil, err
	}
	if u.Scheme == "ldapi" {
		return ldap.Dial("unix", u.Path)
	} else if u.Scheme == "ldap" {
		port := u.Port()
		if port == "" {
			port = "389"
		}
		return ldap.Dial("tcp", net.JoinHostPort(u.Hostname(), port))
	} else if u.Scheme == "ldaps" {
		// build our tls configuration.
		port := u.Port()
		if port == "" {
			port = "636"
		}
		// This should be handled by createTLSConfigFromFlags...
		if serverName != "" {
			tls_config.ServerName = serverName
		} else {
			tls_config.ServerName = u.Hostname()
		}
		return ldap.DialTLS("tcp", net.JoinHostPort(u.Hostname(), port), tls_config)
	}
	return nil, fmt.Errorf("unsupported ldap scheme %v", u.Scheme)
}

func main() {
	flag.Parse()

	tls_config, err := createTLSConfigFromFlags()
	if err != nil {
		log.Fatal(err)
	}
	client, err := createLdapClientFromFlags(*ldap_uri, *ldap_tls_serverName, tls_config)
	if err != nil {
		log.Fatal(err)
	}

	if *ldap_bind != "" {
		if *ldap_password == "" {
			log.Fatal("-ldap.bind given, but -ldap.password wasn't")
		}
		log.Debug("Executing bind")
		err = client.Bind(*ldap_bind, *ldap_password)
		if err != nil {
			log.Fatal(err)
		}
		log.Debug("Bound successfully")
	} else if *ldap_password != "" {
		log.Fatal("-ldap.password given, but -ldap.bind wasn't")
	} else {
		log.Debug("no bind given, thus skipping")
	}

	var sources []*MetricsSource
	if *queryFile != "" {
		log.Debugf("parsing query file %s", *queryFile)
		ms, err := LoadConfigFile(*queryFile)
		if err != nil {
			log.Fatal(err)
		}
		for _, source := range ms {
			sources = append(sources, source)
		}
		log.Debugf("loaded %d queries from configuration", len(sources))
	}

	if !*disableVendorMetrics {
		ms, err := loadBundledMetricsForServer(client)
		if err != nil {
			log.Fatal(err)
		}
		for _, source := range ms {
			sources = append(sources, source)
		}
	}

	if len(sources) == 0 {
		log.Fatal("no metrics were configured; nothing to export")
	}
	e := NewExporter(client, sources)
	prometheus.MustRegister(e)

	log.Infof("starting server; telemetry accessible at %s%s", *listen, *metricsPath)
	http.Handle(*metricsPath, prometheus.Handler())
	log.Fatal(http.ListenAndServe(*listen, nil))
}
