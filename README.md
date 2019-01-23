# LDAP Prometheus Exporter

A [Prometheus](http://prometheus.io) metrics exporter for [LDAP](https://en.wikipedia.org/wiki/Lightweight_Directory_Access_Protocol).

This exporter allows for configurable tree attributes to be exposed as prometheus metrics, and bundles a set of useful metrics for LDAP backends it knows of (currently this is just [389 Directory Server](http://directory.fedoraproject.org/)).

# Build status
[![Build Status](https://travis-ci.org/ferringb/ldap_exporter.svg?branch=master)](https://travis-ci.org/ferringb/ldap_exporter)

## Using

```sh
Usage of ./ldap-exporter:
  -ldap.bind string
    	Ldap DN to bind to
  -ldap.password string
    	LDAP bind DN password
  -ldap.tls-ca-file string
    	If TLS is used, the path for to CA to use
  -ldap.tls.cert-file string
    	If the server requires a client cert, the path to that TLS cert.  If this is passed, -ldap.tls.key-file must also be passed
  -ldap.tls.key-file string
    	If the server requires a client key, the path to that TLS key.  If this is passed, -ldap.tls.cert-file must also be passed
  -ldap.tls.skip-verify
    	If given, do not do any verification of the server's cert.  Insecure and allows for MITM
  -ldap.uri string
    	Openldap compatible URI to connect to.  Can use ldap://, ldaps://, ldapi://
  -log.format value
    	Set the log target and format. Example: "logger:syslog?appname=bob&local=7" or "logger:stdout?json=true" (default "logger:stderr")
  -log.level value
    	Only log messages with the given severity or above. Valid levels: [debug, info, warn, error, fatal] (default "info")
  -metrics.config string
    	YAML file holding ldap -> metrics queries.  Note if the LDAP vendor cannot be identified, this must be set
  -metrics.disable-vendor-metrics
    	By default, try to identify the LDAP vendor and load metrics for thhat vendor.  If the vendor cannot be identified or if this is enabled,, -metrics.config must be set.
  -web.listen-address string
    	The host:port to listen on for HTTP requests (default ":9095")
  -web.telemetry-path string
    	Path under which to expose metrics (default "/metrics")
```
