package meowcaller

import "github.com/rs/zerolog"

// relayEndpointsLog renders offered relays for logs: name, FNA flag and the
// caller-measured RTT only. Tokens, token ids and addresses are left out.
type relayEndpointsLog []relayEndpoint

func (r relayEndpointsLog) MarshalZerologArray(a *zerolog.Array) {
	for i := range r {
		a.Dict(zerolog.Dict().
			Str("relay_name", r[i].relayName).
			Bool("is_fna", r[i].isFNA).
			Uint32("c2r_rtt", r[i].c2rRTT))
	}
}

func relayEndpointNames(eps []relayEndpoint) []string {
	names := make([]string, 0, len(eps))
	for i := range eps {
		names = append(names, eps[i].relayName)
	}
	return names
}
