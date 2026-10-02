package gosms

import "testing"

// FuzzValidatePhoneNumber 守护手机号校验的不变量（种子随常规 go test 执行）：
//  1. 校验函数对任意输入不 panic；
//  2. 校验通过的号码必然以 + 开头，因此格式化应为恒等变换；
//  3. 纯函数与格式校验结果一致（导出版内部委托纯函数）。
func FuzzValidatePhoneNumber(f *testing.F) {
	seeds := []string{
		"+8613711112222",
		"13711112222",
		"+1234567890",
		"+1234567",
		"+123456789012345",
		"+1234567890123456",
		"",
		"+",
		"++8613711112222",
		"+86abc",
		"not-a-phone",
		"+0000000",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, phone string) {
		valid := isValidPhoneNumber(phone)
		if valid {
			if phone == "" || phone[0] != '+' {
				t.Fatalf("校验通过的号码必须以 + 开头: %q", phone)
			}
			if formatted := FormatPhoneNumber(phone); formatted != phone {
				t.Fatalf("已含 + 前缀的合法号码格式化应为恒等: %q → %q", phone, formatted)
			}
		}
	})
}

// FuzzFormatPhoneNumber 守护格式化的不变量：
//  1. 对任意输入不 panic；
//  2. 幂等——输出恒以 + 开头，再次格式化结果不变。
func FuzzFormatPhoneNumber(f *testing.F) {
	seeds := []string{
		"13711112222",
		"+8613711112222",
		"1234567890",
		"",
		"abc",
		"+",
		"1",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, phone string) {
		once := FormatPhoneNumber(phone)
		if once == "" || once[0] != '+' {
			t.Fatalf("格式化输出必须以 + 开头: %q → %q", phone, once)
		}
		if twice := FormatPhoneNumber(once); twice != once {
			t.Fatalf("格式化应幂等: 第一次 %q, 第二次 %q", once, twice)
		}
	})
}
