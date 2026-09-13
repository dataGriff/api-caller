Feature: Authentication
  Each scenario starts with an empty session, so the login step in the
  Background runs for every scenario.

  Background:
    Given I am logged in

  Scenario: The captured token is accepted
    When I ask who I am
    Then the response status is 200
    And the response body "$.authenticated" is "true"

  Scenario: The token from login is visible to later steps
    Then the variable "token" is "{{token}}"
    When I run "bearer-auth"
    Then the response is successful
