package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"strconv"
	"io/ioutil"
	"encoding/json"
	"time"
	"math"
	"strings"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

func checkError(e error) {
	if e != nil {
		fmt.Println("Error: ", e)
		os.Exit(1)
	}
}


var connectHandler mqtt.OnConnectHandler = func(client mqtt.Client) {
    fmt.Println("Connected")
}


var connectLostHandler mqtt.ConnectionLostHandler = func(client mqtt.Client, err error) {
    fmt.Printf("Connect lost: %v\n", err)
}

type Conf struct {
	Mqtt_host string
	Mqtt_port string
	Mqtt_user string
	Mqtt_pass string
	Goe_serial string
}


const tbat  = "pv/inverter/battery/capacity"
const tsol1 = "pv/inverter/solar_input1/power"
const tsol2 = "pv/inverter/solar_input2/power"
const tpow  = "smartmeter/power/total"
var tgoeamp = "go-eCharger/%s/amp"
var tgoefrc = "go-eCharger/%s/frc"
var tgoepsm = "go-eCharger/%s/psm"
var tgoenrg = "go-eCharger/%s/nrg"
var tgoemca = "go-eCharger/%s/mca"
const ema   = 0.05
const pow_min = 1380    // = 6A * 230V
const tdiff_min  = 60.  // 2 * pow_min time diff
const tdiff_min2 = 300. // 1 * pow_min time diff

type Energy struct {
	bat  int
	sol1 int
	sol2 int
	pow  float64
	mca  int
	nrgp int

	chgpowf float64
	chgpow  int
	chgtime time.Time
	active  bool
}


func enable_charger(client mqtt.Client, enable bool) {

	var frc = 1

	if enable {
		frc = 2
	}

	client.Publish(tgoefrc, 1, false, frc)
}


func write_charge_power(client mqtt.Client, power int, mca int) {

	var ampere =  power / 230
	var phasmod = "1"

	if ampere > 16 {
		phasmod = "2"
		ampere = ampere / 3
	}

	if ampere < mca {
		ampere = mca
	}

	client.Publish(tgoepsm, 1, false, phasmod)
	client.Publish(tgoeamp, 1, false, ampere);
}


func processMessage(energy *Energy, msg mqtt.Message, client mqtt.Client) {
	topic := msg.Topic()
	payload := string(msg.Payload())
	switch {
	case topic == tbat:
		energy.bat, _  = strconv.Atoi(payload)
	case topic == tsol1:
		energy.sol1, _ = strconv.Atoi(payload)
	case topic == tsol2:
		energy.sol2, _ = strconv.Atoi(payload)
	case topic == tgoemca:
		energy.mca, _  = strconv.Atoi(payload)
	case topic == tgoenrg:
		s := strings.Split(payload, ",")
		if len(s) > 11 {
			energy.nrgp, _ = strconv.Atoi(s[11])
		}
	case topic == tpow:
		energy.pow, _  = strconv.ParseFloat(payload,64)

		// energy.pow = power of whole house, including charger
		var chgcur = float64(energy.sol1 + energy.sol2) - energy.pow +
				float64(energy.nrgp)

		if energy.bat > 90 {
			energy.chgpowf = energy.chgpowf +
				(chgcur - energy.chgpowf) * ema
		} else {
			energy.chgpowf = 0
		}

		fmt.Printf("charge power float %f\n", energy.chgpowf)
//                 fmt.Println(energy)

		var now  = time.Now()
		var tdiff = now.Sub(energy.chgtime).Seconds()
		if tdiff < tdiff_min {
			return
		}

		var diff  = math.Abs(float64(energy.chgpow) - energy.chgpowf)
		var change  = (tdiff >= tdiff_min  && diff >= pow_min * 2) ||
			      (tdiff >= tdiff_min2 && diff >= pow_min)
		if change {
			energy.chgpow = int(energy.chgpowf)
			energy.chgtime = now
			fmt.Printf("Change to %d\n", energy.chgpow)

			if energy.active && energy.chgpow <= pow_min / 2 {
				fmt.Printf("Switch OFF\n")
				energy.active = false
				enable_charger(client, false)
			} else if !energy.active  && energy.chgpow >=
							pow_min * 3 / 2 {
				fmt.Printf("Switch ON\n")
				energy.active = true 
				enable_charger(client, true)
			}

			if energy.active && energy.chgpow >= 0 {
				fmt.Printf("Set charge power to %d\n",
					energy.chgpow)
				write_charge_power(client, energy.chgpow,
						   energy.mca)
			}
		}
	}
}


func mqttMessageHandler(energy *Energy) mqtt.MessageHandler {
	return func(client mqtt.Client, msg mqtt.Message) {

		processMessage(energy, msg, client)
	}
}


func mainloop() {
    c := make(chan os.Signal)
    signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)

    fmt.Printf("BEGIN\n")
    // block until a signal is received
    s := <-c
    fmt.Println("Got signal:", s)
    fmt.Printf("END\n")
}


func main() {
	var rc    = ".energydrc"

	var energy Energy
	energy.mca = 6

	file, err := ioutil.ReadFile(rc)
	if err != nil {
		fmt.Printf("Could not read %s\n", rc, err)
		os.Exit(1)
	}

	var cfg Conf
	err = json.Unmarshal(file, &cfg)
	if err != nil {
		fmt.Printf("Wrong format %s\n", rc, err)
		os.Exit(1)
	}

	fmt.Printf("broker: %s:%s\n", cfg.Mqtt_host, cfg.Mqtt_port);

	opts := mqtt.NewClientOptions()
	opts.AddBroker(fmt.Sprintf("tcp://%s:%s",
			cfg.Mqtt_host, cfg.Mqtt_port))
	opts.SetUsername(cfg.Mqtt_user)
	opts.SetPassword(cfg.Mqtt_pass)
	opts.OnConnect = connectHandler
	opts.OnConnectionLost = connectLostHandler
	client := mqtt.NewClient(opts)

	if token := client.Connect(); token.Wait() {
		checkError(token.Error())
	}

	tgoeamp = fmt.Sprintf(tgoeamp, cfg.Goe_serial)
	tgoefrc = fmt.Sprintf(tgoefrc, cfg.Goe_serial)
	tgoepsm = fmt.Sprintf(tgoepsm, cfg.Goe_serial)
	tgoenrg = fmt.Sprintf(tgoenrg, cfg.Goe_serial)
	tgoemca = fmt.Sprintf(tgoemca, cfg.Goe_serial)

	client.Subscribe(tbat,    1, mqttMessageHandler(&energy))
	client.Subscribe(tsol1,   1, mqttMessageHandler(&energy))
	client.Subscribe(tsol2,   1, mqttMessageHandler(&energy))
	client.Subscribe(tpow,    1, mqttMessageHandler(&energy))
	client.Subscribe(tgoemca, 1, mqttMessageHandler(&energy))
	client.Subscribe(tgoenrg, 1, mqttMessageHandler(&energy))

	mainloop()

	client.Disconnect(250)
}

