@fixture
Feature: Ping
  The fixture's one feature, for the Test Explorer tests.

  Scenario: The API answers
    When I run "ping"
    Then the response status is 200

  Scenario: Deep
    Given the deep request runs
    Then the response status is 200
