// TOKEN is the output of: gcloud auth print-access-token
const response = await fetch("https://api.example.com/me", {
  method: "GET",
  headers: {
    "X-Token": process.env.TOKEN,
  },
});
console.log(response.status);
console.log(await response.text());
