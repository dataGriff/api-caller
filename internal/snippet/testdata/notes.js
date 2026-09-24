// TOKEN is an OAuth2 access token from https://idp/token
// apic's TLS settings are not carried over: client cert certs/client.pem · ca certs/ca.pem
// the request line asks for HTTP/2, which this client does not require
const response = await fetch("https://cdn.example.com/x", {
  method: "PURGE",
  headers: {
    "Authorization": "Bearer " + process.env.TOKEN,
  },
});
console.log(response.status);
console.log(await response.text());
