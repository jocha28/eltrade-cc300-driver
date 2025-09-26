package cmd

import (
	"fmt"
	"github.com/geeckmc/eltrade-cc300-driver/lib"
	"strings"
	"time"
	"strconv"
)

type DeviceInfo struct {
	NIM                    string
	IFU                    string
	TIME                   string
	COUNTER                string
	SellBillCounter        string
	SettlementBillCounter  string
	TaxA                   string
	TaxB                   string
	TaxC                   string
	TaxD                   string
	CompanyName            string
	CompanyLocationAddress string
	CompanyLocationCity    string
	CompanyContactPhone    string
	CompanyContactEmail    string
	LastConnectionToServer string
	DocumentOnDeviceCount  string
	UploadedDocumentCount  string
}

var (
	timeZone, _ = time.LoadLocation("Africa/Porto-Novo")
)

func GetDeviceState(dev *eltrade.Device) (DeviceInfo, error) {
	r := dev.Send(eltrade.NewRequest(eltrade.DEV_STATE))
	deviceInfo := DeviceInfo{}
	data, err := r.GetData()
	if err != nil {
		return deviceInfo, err
	}
	dataSlice := strings.Split(data, string(eltrade.RESPONSE_DELIMITER))
	eltrade.Logger.Debugf("fn:DeviceState -- command response %v", dataSlice)
	if len(dataSlice) < 10 {
		return deviceInfo, fmt.Errorf("invalid device state response: too few fields")
	}
	deviceInfo.NIM = dataSlice[0]
	deviceInfo.IFU = dataSlice[1]
	formattedDate, _ := time.ParseInLocation("20060102150405", dataSlice[2], timeZone)
	deviceInfo.TIME = formattedDate.String()
	deviceInfo.COUNTER = dataSlice[3]
	deviceInfo.SellBillCounter = dataSlice[4]
	deviceInfo.SettlementBillCounter = dataSlice[5]
	deviceInfo.TaxA = dataSlice[6]
	deviceInfo.TaxB = dataSlice[7]
	deviceInfo.TaxC = dataSlice[8]
	deviceInfo.TaxD = dataSlice[9]
	return deviceInfo, nil
}

func GetTaxServerState(dev *eltrade.Device) (DeviceInfo, error) {
	r := dev.Send(eltrade.NewRequest(eltrade.NETWORK_STATE))
	deviceInfo := DeviceInfo{}
	data, err := r.GetData()
	if err != nil {
		return deviceInfo, err
	}
	dataSlice := strings.Split(data, string(eltrade.RESPONSE_DELIMITER))
	eltrade.Logger.Debugf("fn:TaxServerState -- command response %v", dataSlice)
	if len(dataSlice) >= 3 {
		deviceInfo.UploadedDocumentCount = dataSlice[0]
		deviceInfo.DocumentOnDeviceCount = dataSlice[1]
		formattedDate, _ := time.ParseInLocation("20060102150405", dataSlice[2], timeZone)
		deviceInfo.LastConnectionToServer = formattedDate.String()
	}
	return deviceInfo, nil
}

func GetTaxPayerInfo(dev *eltrade.Device) (DeviceInfo, error) {
	req := eltrade.NewRequest(eltrade.TAXPAYER_INFO)
	deviceInfo := DeviceInfo{}
	for i := 0; i <= 5; i++ {
		req.Body(fmt.Sprintf("I%d", i))
		r := dev.Send(req)
		data, err := r.GetData()
		eltrade.Logger.Debugf("fn:TaxPayerInfo -- command response I%d: %s", i, data)
		if err != nil {
			return deviceInfo, err
		}
		switch i {
		case 0:
			deviceInfo.CompanyName = data
		case 1:
			deviceInfo.CompanyLocationAddress = data
		case 2:
			deviceInfo.CompanyLocationAddress = deviceInfo.CompanyLocationAddress + " " + data
		case 3:
			deviceInfo.CompanyLocationCity = data
		case 4:
			deviceInfo.CompanyContactPhone = data
		case 5:
			deviceInfo.CompanyContactEmail = data
		}
	}
	return deviceInfo, nil
}

func CreateBill(dev *eltrade.Device, json []byte) (string, error) {
	bill, err := newBillFromJson(json)
	if err != nil {
		eltrade.Logger.Errorf("fn:Cmd:CreateBill -- %v", err)
		return "", err
	}

	devInfo, err := GetDeviceState(dev)
	if err != nil {
		eltrade.Logger.Errorf("fn:Cmd:CreateBill -- %v", err)
		return "", err
	}

	// Ouvrir la facture
	req := eltrade.NewRequest(eltrade.START_BILL)
	eltradeString := eltrade.EltradeString{}
	eltradeString.Append(bill.SellerId)
	eltradeString.Append(bill.SellerName)
	eltradeString.Append(devInfo.IFU)
	eltradeString.Append(devInfo.TaxA)
	eltradeString.Append(devInfo.TaxB)
	eltradeString.Append(devInfo.TaxC)
	eltradeString.Append(devInfo.TaxD)
	eltradeString.Append(bill.VT)
	eltradeString.Append(bill.RT)
	eltradeString.Append(bill.RN)
	eltradeString.Append(bill.BuyerIFU)
	eltradeString.Append(bill.BuyerName)
	if bill.AIB != "N/A" {
		eltradeString.Append(bill.AIB)
	}
	req.Body(eltradeString.Val)
	r := dev.Send(req)
	res, err := r.GetData()
	if err != nil {
		eltrade.Logger.Errorf("fn:Cmd:CreateBill -- START_BILL failed: %v", err)
		return "", err
	}
	if strings.Contains(res, "E:") {
		return "", fmt.Errorf("device initialization failed: %s", res)
	}

	// Ajout des articles
	for _, product := range bill.Products {
		req = eltrade.NewRequest(eltrade.ADD_BILL_ITEM)
		eltradeString = eltrade.EltradeString{}
		eltradeString.Val = clear(product.Label)
		if strings.TrimSpace(product.BarCode) != "" {
			eltradeString.Val += "\n" + clear(strings.TrimSpace(product.BarCode))
		}
		eltradeString.Val += "\t" + product.Tax
		eltradeString.Val += fmt.Sprintf("%f", product.Price)
		eltradeString.Val += fmt.Sprintf("*%f", product.Items)
		if product.SpecificTax > 0 {
			eltradeString.Val += fmt.Sprintf(";%f,%s", product.SpecificTax, clear(product.SpecificTaxDesc))
		}
		if product.OriginalPrice > 0 {
			eltradeString.Val += fmt.Sprintf("\t%f,%s", product.OriginalPrice, clear(product.PriceChangeExplanation))
		}
		req.Body(eltradeString.Val)
		r = dev.Send(req)
		res, err = r.GetData()
		if err != nil {
			eltrade.Logger.Errorf("fn:Cmd:CreateBill -- ADD_BILL_ITEM failed: %v", err)
			return "", err
		}
	}

	// Vérification sous-total (33h)
	req = eltrade.NewRequest(eltrade.GET_BILL_SUB_TOTAL)
	r = dev.Send(req)
	res, err = r.GetData()
	if err != nil {
		eltrade.Logger.Errorf("fn:Cmd:CreateBill -- GET_BILL_SUB_TOTAL failed: %v", err)
		return "", err
	}
	subTotalParts := strings.Split(res, ",")
	if subTotalParts[0] != "P" || len(subTotalParts) < 4 {
		return "", fmt.Errorf("sous-total invalide: %s", res)
	}
	// TODO: Calculer total local et comparer avec subTotalParts[3] (TTC)

	// Total et paiements (35h)
	req = eltrade.NewRequest(eltrade.GET_BILL_TOTAL)
	var remaining float64
	for _, payment := range bill.Payments {
		eltradeString = eltrade.EltradeString{}
		eltradeString.Val = fmt.Sprintf("%s%f", payment.Mode, payment.Amount)
		req.Body(eltradeString.Val)
		r = dev.Send(req)
		res, err = r.GetData()
		if err != nil {
			eltrade.Logger.Errorf("fn:Cmd:CreateBill -- GET_BILL_TOTAL failed: %v", err)
			return "", err
		}
		parts := strings.Split(res, ",")
		if parts[0] != "P" || len(parts) < 5 {
			return "", fmt.Errorf("paiement échoué: %s", res)
		}
		remaining, err = strconv.ParseFloat(parts[4], 64)
		if err != nil {
			eltrade.Logger.Errorf("fn:Cmd:CreateBill -- Parse remaining failed: %v", err)
			return "", err
		}
	}
	if remaining > 0 {
		return "", fmt.Errorf("paiements insuffisants: restant %f", remaining)
	}

	// Finaliser avec la commande 38h
	req = eltrade.NewRequest(0x38) // Commande 38h
	req.Body("")
	r = dev.Send(req)
	res, err = r.GetData()
	if err != nil {
		eltrade.Logger.Errorf("fn:Cmd:CreateBill -- Command 38h failed: %v", err)
		return "", err
	}
	splitedRes := strings.Split(res, ";")
	if len(splitedRes) < 5 || splitedRes[0] != "F" {
		return "", fmt.Errorf("fin de facture échouée, réponse invalide: %s", res)
	}
	// Format: F;<NIM>;<SIG>;<IFU>;<DT>
	fiscal_response := fmt.Sprintf("F;%s;%s;%s;%s", splitedRes[1], splitedRes[2], splitedRes[3], splitedRes[4])

	// Exécuter END_BILL pour fermer la session
	req = eltrade.NewRequest(eltrade.END_BILL)
	req.Body("")
	r = dev.Send(req)
	res, err = r.GetData()
	if err != nil {
		eltrade.Logger.Errorf("fn:Cmd:CreateBill -- END_BILL failed: %v", err)
		return "", err
	}

	return fiscal_response, nil
}

func clear(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\n", ""), "\t", "")
}