package stripe

import "strings"

func currencyDecimals(currency string) int {
	switch strings.ToUpper(currency) {
	case "BIF", "CLP", "DJF", "GNF", "JPY", "KMF", "KRW", "MGA", "PYG", "RWF", "UGX", "VND", "VUV", "XAF", "XOF", "XPF":
		return 0
	case "BHD", "JOD", "KWD", "OMR", "TND":
		return 3
	default:
		return 2
	}
}

func RoundHundredths(amount int64, currency string) int64 {
	if currencyDecimals(currency) == 0 {
		return (amount + 50) / 100 * 100
	}
	return amount
}

func ToStripeAmount(amount int64, currency string) int64 {
	switch currencyDecimals(currency) {
	case 0:
		return amount / 100
	case 3:
		return amount * 10
	default:
		return amount
	}
}

func FromStripeAmount(amount int64, currency string) int64 {
	switch currencyDecimals(currency) {
	case 0:
		return amount * 100
	case 3:
		return amount / 10
	default:
		return amount
	}
}
