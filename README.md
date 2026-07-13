# Northumberland Bins for Home Assistant

A Home Assistant app that retrieves upcoming bin collection dates & types from Northumberland County Council.

## Requirements

- Home Assistant OS or another installation with app support
- A valid property UPRN
- Access to edit the Home Assistant YAML configuration

## Find your UPRN

Use [uprn.uk/map](https://uprn.uk/map) or a similar service to find the UPRN for the property.

Northumberland County Council uses a 12-digit address identifier derived from the standard 11-digit UPRN. Add a leading `0` before entering the value into the app.

For example:

```text
UPRN:
10001018389

Value required by Northumberland County Council:
010001018389
````

## Install the app

Open:

```text
Settings → Apps → App store
```

Open the repository menu in the top-right and add:

```text
https://github.com/TeddiO/northumberland-bins-collection-home-assistant
```

Refresh the app store if necessary.

Find **Northumberland Bins** and install it.

## Configure the app

Open the installed **Northumberland Bins** app and select the **Configuration** tab.

Enter the 12-digit address identifier:

```yaml
uprn: "<your UPRN>"
```

Save the configuration and start the app.

The app log should show:

```text
listening on :8080
```

## Configure the sensors

Home Assistant must connect to the app using its internal hostname.

Open the installed **Northumberland Bins** app and note the internal hostname shown by Home Assistant.

Using a file editor, create:

```text
/config/northumberland_bins.yaml
```

Add the following (`sample-config.yaml`), replacing `<app-hostname>` with the hostname assigned by Home Assistant:

```yaml
- resource: http://<app-hostname>:8080/collections
  scan_interval: 21600
  timeout: 60

  sensor:
    - name: Next Bin Collection
      unique_id: northumberland_next_bin_collection
      device_class: date
      value_template: >-
        {{ value_json.collections
           | map(attribute='date')
           | first
           | default('unknown', true) }}
      json_attributes:
        - address
        - status
        - available
        - warning
        - fetched_at
        - collections

    - name: Next General Waste Collection
      unique_id: northumberland_next_general_waste_collection
      device_class: date
      value_template: >-
        {{ value_json.collections
           | selectattr('type', 'equalto', 'general')
           | map(attribute='date')
           | first
           | default('unknown', true) }}

    - name: Next Recycling Collection
      unique_id: northumberland_next_recycling_collection
      device_class: date
      value_template: >-
        {{ value_json.collections
           | selectattr('type', 'equalto', 'recycling')
           | map(attribute='date')
           | first
           | default('unknown', true) }}

    - name: Next Garden Waste Collection
      unique_id: northumberland_next_garden_waste_collection
      device_class: date
      value_template: >-
        {{ value_json.collections
           | selectattr('type', 'equalto', 'garden')
           | map(attribute='date')
           | first
           | default('unknown', true) }}
```

Add this to `configuration.yaml`:

```yaml
rest: !include config/northumberland_bins.yaml
```

Check the configuration, then restart Home Assistant.

## Enable the sensors

Open:

```text
Settings → Devices & services → Entities
```

Search for:

```text
Northumberland
```

Enable the sensors if Home Assistant created them in a disabled state.

The following entities should then be available:

```text
sensor.next_bin_collection
sensor.next_general_waste_collection
sensor.next_recycling_collection
sensor.next_garden_waste_collection
```

The individual waste sensors contain the next collection date for each waste type.

`Next Bin Collection` also contains the address, status and complete collection list as attributes.

## Polling

Home Assistant requests updated collection data every six hours:

```yaml
scan_interval: 21600
```

## Troubleshooting

### The app says that the UPRN is required

Open the app’s **Configuration** tab and confirm that the UPRN has been saved:

```yaml
uprn: "<your UPRN>"
```

Restart the app after saving the configuration.

### No collections are returned

Confirm that:

* the property is within Northumberland
* the UPRN is correct
* the leading `0` has been added
* the app log shows `listening on :8080`
* Check that NCC have valid data for your location [here](https://bincollection.northumberland.gov.uk/postcode)

### The sensors are unavailable

Confirm that:

* the app is running
* the hostname in `northumberland_bins.yaml` matches the hostname assigned by Home Assistant
* the endpoint uses port `8080` and the `/collections` path

### Browser validation is required

Northumberland County Council may occasionally require browser validation or display a CAPTCHA.

When this occurs, the app cannot refresh the collection data and will report a `validation_required` status until the council website allows normal requests again.
