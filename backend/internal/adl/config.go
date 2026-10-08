package adl

import (
	"os"
	"strings"
)

// Output names accepted in Config.Output, comma-separated.
const (
	OutputStdout = "stdout"
	OutputOTLP   = "otlp"
)

// Config says where records go. The zero value writes them to stdout only.
type Config struct {
	// Output is a comma-separated list of stdout and otlp. Empty means stdout.
	Output string
	// OTLPEndpoint is the collector's OTLP/gRPC address, such as alloy.observability.svc.cluster.local:4317.
	OTLPEndpoint string
	// OTLPInsecure sends without TLS, for a collector inside the cluster.
	OTLPInsecure bool
	// Resource identifies the producer of the records. service.name defaults to attesta and instance_id to the host name.
	Resource map[string]string
}

// ConfigFromEnv reads ATTESTA_ADL_OUTPUT, ATTESTA_ADL_OTLP_ENDPOINT, ATTESTA_ADL_OTLP_INSECURE (true) and ATTESTA_ADL_RESOURCE (key=value,key=value).
func ConfigFromEnv() Config {
	return Config{
		Output:       os.Getenv("ATTESTA_ADL_OUTPUT"),
		OTLPEndpoint: os.Getenv("ATTESTA_ADL_OTLP_ENDPOINT"),
		OTLPInsecure: os.Getenv("ATTESTA_ADL_OTLP_INSECURE") == "true",
		Resource:     parseResource(os.Getenv("ATTESTA_ADL_RESOURCE")),
	}
}

func parseResource(value string) map[string]string {
	out := map[string]string{}
	for _, entry := range strings.Split(value, ",") {
		key, val, ok := strings.Cut(entry, "=")
		if key = strings.TrimSpace(key); ok && key != "" {
			out[key] = strings.TrimSpace(val)
		}
	}
	return out
}

func (c Config) outputs() map[string]bool {
	raw := c.Output
	if strings.TrimSpace(raw) == "" {
		raw = OutputStdout
	}
	set := map[string]bool{}
	for _, o := range strings.Split(raw, ",") {
		if o = strings.TrimSpace(o); o != "" {
			set[o] = true
		}
	}
	return set
}

// effectiveResource adds service.name and instance_id unless the deployment named them; without them the records of replicas and of separate
// deployments cannot be told apart once aggregated.
func (c Config) effectiveResource() map[string]string {
	resource := map[string]string{}
	for k, v := range c.Resource {
		resource[k] = v
	}
	if _, ok := resource["service.name"]; !ok {
		resource["service.name"] = "attesta"
	}
	if _, ok := resource["instance_id"]; !ok {
		host, err := os.Hostname()
		if err != nil || host == "" {
			host, _ = randomHex(8)
		}
		resource["instance_id"] = host
	}
	return resource
}
