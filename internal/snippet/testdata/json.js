const response = await fetch("https://api.example.com/todos?list=home&q=it's", {
  method: "POST",
  headers: {
    "Content-Type": "application/json",
    "Accept": "application/json",
    "X-Note": "say \"hi\" $HOME",
    "Authorization": "Bearer s3cret-token",
  },
  body: "{\"title\": \"café \\\"x\\\"\",\n \"done\": false}",
});
console.log(response.status);
console.log(await response.text());
