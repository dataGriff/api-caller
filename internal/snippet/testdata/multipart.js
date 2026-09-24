import { readFile } from "node:fs/promises";

const form = new FormData();
form.append("title", "Q3 report");
form.append("file", new Blob([await readFile("docs/report.pdf")], { type: "application/pdf" }), "q3.pdf");
const response = await fetch("http://localhost:8080/upload", {
  method: "POST",
  headers: {
    "Authorization": "Basic " + btoa("alice" + ":" + "pa'ss"),
  },
  body: form,
});
console.log(response.status);
console.log(await response.text());
