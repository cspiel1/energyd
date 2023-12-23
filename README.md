# go-eCharger controller

Controls a go-eCharger such that the car is charged with solar power. It uses
the MQTT API of the go-eCharger and collects data from the power converter and
a smartmeter via the MQTT topics

- `pv/inverter/battery/capacity`
- `pv/inverter/solar_input1/power`
- `pv/inverter/solar_input2/power`
- `smartmeter/power/total`

Usage:
```
cp energydrc-template .energydrc
```
Then edit .energydrc !
