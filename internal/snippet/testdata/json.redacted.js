const response = await fetch("https://api.example.com/todos?list=***&q=***", {
  method: "POST",
  headers: {
    "Content-Type": "***",
    "Accept": "***",
    "X-Note": "***",
    "Authorization": "Bearer " + process.env.TOKEN,
  },
  body: "***",
});
console.log(response.status);
console.log(await response.text());
