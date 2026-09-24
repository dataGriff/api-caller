// apic's TLS settings are not carried over: server certificate not verified
// apic sends this through the proxy http://proxy.internal:3128
const url = new URL("https://internal.example.com/v1/items");
url.searchParams.append("api_key", process.env.APIC_API_KEY);
const response = await fetch(url, {
  method: "GET",
});
console.log(response.status);
console.log(await response.text());
