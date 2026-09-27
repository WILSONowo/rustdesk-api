# Working agreement

- Make changes, build, and verify in the local environment first. Keep the local frontend and backend compatible.
- Let the user test locally before publishing changes.
- Do not push to GitHub or deploy to the server unless the user explicitly approves that action after local testing. Earlier deployment authorization does not carry over to later changes.
- Keep SMTP credentials and deployment secrets outside Git. Do not print them in logs or reports.
