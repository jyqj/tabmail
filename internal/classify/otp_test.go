package classify

import "testing"

func TestOTPFromMessage(t *testing.T) {
	cases := []struct {
		name string
		env  Env
		want OTPResult
	}{
		{
			name: "empty env is zero-value no panic",
			env:  Env{},
			want: OTPResult{},
		},
		{
			name: "subject keyword adjacent wins high confidence",
			env:  Env{Subject: "Your verification code is 123456"},
			want: OTPResult{Code: "123456", Confidence: otpConfidenceKeyword, Found: true, Strategy: "keyword_adjacent"},
		},
		{
			name: "text body keyword adjacent",
			env:  Env{Subject: "Login", TextBody: "Use this OTP: 042193 to sign in"},
			want: OTPResult{Code: "042193", Confidence: otpConfidenceKeyword, Found: true, Strategy: "keyword_adjacent"},
		},
		{
			name: "CJK keyword in subject",
			env:  Env{Subject: "您的验证码：778812，10 分钟内有效"},
			want: OTPResult{Code: "778812", Confidence: otpConfidenceKeyword, Found: true, Strategy: "keyword_adjacent"},
		},
		{
			name: "CJK 动态码 keyword in body",
			env:  Env{Subject: "通知", TextBody: "动态码 654321 请勿泄露"},
			want: OTPResult{Code: "654321", Confidence: otpConfidenceKeyword, Found: true, Strategy: "keyword_adjacent"},
		},
		{
			name: "html only strips tags then keyword match",
			env:  Env{HTMLBody: `<html><body><b>Your code is</b> <span>918273</span></body></html>`},
			want: OTPResult{Code: "918273", Confidence: otpConfidenceKeyword, Found: true, Strategy: "keyword_adjacent"},
		},
		{
			name: "digit run fallback low confidence",
			env:  Env{TextBody: "Order confirmed. Reference 482910 available now."},
			want: OTPResult{Code: "482910", Confidence: otpConfidenceDigit, Found: true, Strategy: "digit_run"},
		},
		{
			name: "amazon order noise picks shortest plausible run",
			env:  Env{TextBody: "Order #112-9482721-3356401 shipped."},
			want: OTPResult{Code: "9482721", Confidence: otpConfidenceDigit, Found: true, Strategy: "digit_run"},
		},
		{
			name: "stripe amount only is still digit_run fallback",
			env:  Env{TextBody: "You paid $42.50. Receipt 1005"},
			want: OTPResult{Code: "1005", Confidence: otpConfidenceDigit, Found: true, Strategy: "digit_run"},
		},
		{
			name: "no digits at all returns not found",
			env:  Env{Subject: "Hello there", TextBody: "Just a normal message with no numbers."},
			want: OTPResult{},
		},
		{
			name: "keyword prefers adjacent over distant noise digit",
			env:  Env{TextBody: "Order 9923847. Your verification code is 314159. Reply STOP."},
			want: OTPResult{Code: "314159", Confidence: otpConfidenceKeyword, Found: true, Strategy: "keyword_adjacent"},
		},
		{
			name: "too short digits ignored",
			env:  Env{TextBody: "I have 12 apples and 3 oranges."},
			want: OTPResult{},
		},
		{
			name: "html script block digits are not picked as OTP",
			env:  Env{HTMLBody: `<script>var code=9999;</script>verification code 4242`},
			want: OTPResult{Code: "4242", Confidence: otpConfidenceKeyword, Found: true, Strategy: "keyword_adjacent"},
		},
		{
			name: "html style block digits are not picked as OTP",
			env:  Env{HTMLBody: `<style>.x{width:123456px}</style>Your code: 555000`},
			want: OTPResult{Code: "555000", Confidence: otpConfidenceKeyword, Found: true, Strategy: "keyword_adjacent"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := OTPFromMessage(tc.env)
			if got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestOTPFromMessageDeterministic asserts the purity invariant: identical
// inputs yield identical outputs across repeated calls (no time/random/IO).
func TestOTPFromMessageDeterministic(t *testing.T) {
	env := Env{Subject: "Verification code 284917", TextBody: "extra noise 9923847"}
	first := OTPFromMessage(env)
	for i := 0; i < 5; i++ {
		got := OTPFromMessage(env)
		if got != first {
			t.Fatalf("non-deterministic: %+v then %+v", first, got)
		}
	}
	if !first.Found || first.Code != "284917" || first.Strategy != "keyword_adjacent" {
		t.Fatalf("unexpected result %+v", first)
	}
}
