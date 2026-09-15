Feature: Spotify search
  The requests live in search.http; the phrases used here are declared on
  them with "# @step". Needs a real client id/secret in
  http-client.private.env.json (see README.md at the repo root for how to
  register a Spotify app).

  Scenario: Search returns an artist
    When I search Spotify for an artist
    Then the response status is 200
    And the response body "$.artists.items[0].name" exists

  Scenario: Browsing new releases works
    When I browse new releases
    Then the response is successful
