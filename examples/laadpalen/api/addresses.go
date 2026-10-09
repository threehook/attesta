package main

import "fmt"

// The address data is the app's own business data, not something Attesta knows about: whether an address exists, already has a laadpaal, and has an
// electric vehicle. Attesta decides who may submit a request; these rules decide what happens to it.
type address struct {
	laadpaalPresent bool
	electricVehicle bool
}

var addresses = map[string]address{
	"1111AA-1": {laadpaalPresent: true, electricVehicle: true},
	"1111BB-2": {electricVehicle: true},
	"1111DD-4": {},
}

// result is what became of a request an employee was authorized to submit.
type result struct {
	Granted bool   `json:"granted"`
	Reason  string `json:"reason"`
}

func decideRequest(postcode string, houseNumber int) result {
	a, ok := addresses[fmt.Sprintf("%s-%d", postcode, houseNumber)]
	switch {
	case !ok:
		return result{Reason: "Postcode/huisnummer niet gevonden"}
	case a.laadpaalPresent:
		return result{Reason: "Reeds laadpaal aanwezig"}
	case !a.electricVehicle:
		return result{Reason: "Geen elektrisch voertuig gevonden op adres"}
	}
	return result{Granted: true, Reason: "Toegekend"}
}
