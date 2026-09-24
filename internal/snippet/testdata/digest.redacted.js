// the API uses HTTP digest auth, which this snippet does not answer; apic does
const response = await fetch("https://api.example.com/items/7", {
  method: "DELETE",
});
console.log(response.status);
console.log(await response.text());
