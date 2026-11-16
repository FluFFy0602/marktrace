package main

// Own GTIN / code helpers: the generator must not depend on backend code (SPEC §9).

// gs1CheckDigit returns the GS1 mod-10 check digit for body (all digits except
// the check digit). Weights are 3,1,3,... starting from the rightmost body digit.
func gs1CheckDigit(body string) byte {
	sum, w := 0, 3
	for i := len(body) - 1; i >= 0; i-- {
		sum += int(body[i]-'0') * w
		w = 4 - w
	}
	return byte('0' + (10-sum%10)%10)
}

// normGTIN converts a GTIN-13 to its GTIN-14 comparison form; GTIN-14 is returned as is.
func normGTIN(g string) string {
	if len(g) == 13 {
		return "0" + g
	}
	return g
}

// makeCode builds a marking code: "01" + GTIN-14 + "21" + 6-char serial.
func makeCode(gtin, serial string) string {
	return "01" + normGTIN(gtin) + "21" + serial
}
