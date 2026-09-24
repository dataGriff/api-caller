// sign this request with AWS Signature V4 (service execute-api, region eu-west-2); apic does it with the AWS credential chain
const response = await fetch("https://abc.execute-api.eu-west-2.amazonaws.com/prod/orders", {
  method: "GET",
});
console.log(response.status);
console.log(await response.text());
