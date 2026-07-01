package interp

import "testing"

func TestSmoke(t *testing.T) {
	cases := []struct{ src, want string }{
		{`1 + 2 * 3`, "7"},
		{`"a" + "b" + 1`, "ab1"},
		{`let x = [1,2,3]; x.map(n => n*n).join(",")`, "1,4,9"},
		{`function f(a,b){return a+b} f(2,3)`, "5"},
		{`const o = {a:1, get b(){return 2}}; o.a + o.b`, "3"},
		{`class A{constructor(x){this.x=x} get(){return this.x}} new A(9).get()`, "9"},
		{`class B extends Object{constructor(){super(); this.y=5}} new B().y`, "5"},
		{`let s=0; for(let i=0;i<5;i++) s+=i; s`, "10"},
		{`let r=[]; for(const v of [10,20,30]) r.push(v); r.join("-")`, "10-20-30"},
		{`JSON.stringify({a:1,b:[2,3]})`, `{"a":1,"b":[2,3]}`},
		{`JSON.parse('{"x":5}').x`, "5"},
		{`try { null.x } catch(e){ e.name }`, "TypeError"},
		{`[1,2,3,4].filter(x=>x%2==0).reduce((a,b)=>a+b,0)`, "6"},
		{`Math.max(1,5,3) + Math.min(2,8)`, "7"},
		{`(a=>b=>a+b)(3)(4)`, "7"},
		{`let {a, b=9} = {a:1}; a+b`, "10"},
		{`let [x,,z] = [1,2,3]; x+z`, "4"},
		{"`sum=${1+2}`", "sum=3"},
		{`typeof undefined + " " + typeof 5 + " " + typeof "s"`, "undefined number string"},
		{`10 > 5 ? "yes" : "no"`, "yes"},
		{`null ?? "default"`, "default"},
		{`[..."abc"].length`, "3"},
	}
	i := New()
	for _, c := range cases {
		v, err := i.RunString("test", c.src)
		if err != nil {
			t.Errorf("%q: error %v", c.src, err)
			continue
		}
		got, _ := i.ToStringV(i.ctx, v)
		if got != c.want {
			t.Errorf("%q = %q, want %q", c.src, got, c.want)
		}
	}
}
