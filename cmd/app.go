package cmd

import (
	"fmt"
	"github.com/geeckmc/eltrade-cc300-driver/lib"
	"strings"
	"time"
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
	eltrade.Logger.Debugf("fn:DeviceState -- command response ", dataSlice)
	if len(dataSlice) >= 10 {
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
	}
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
	eltrade.Logger.Debugf("fn:TaxServerState -- command response ", dataSlice)
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
		eltrade.Logger.Debugf("fn:TaxPayerInfo -- command response ", string(req.Data), data)
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

	req := eltrade.NewRequest(eltrade.START_BILL)
	eltradeString := eltrade.EltradeString{Val: bill.SellerId}
	eltradeString.Append(bill.SellerName)
	eltradeString.Append(bill.BuyerIFU) // Utiliser BuyerIFU au lieu de devInfo.IFU
	eltradeString.Append(devInfo.TaxA)
	eltradeString.Append(devInfo.TaxB)
	eltradeString.Append(devInfo.TaxC)
	eltradeString.Append(devInfo.TaxD)
	eltradeString.Append(bill.VT)
	if bill.RT != "" {
		eltradeString.Append(bill.RT)
	}
	if bill.RN != "" {
		eltradeString.Append(bill.RN)
	}
	eltradeString.Append(bill.BuyerName)
	if bill.AIB != "N/A" {
		eltradeString.Append(bill.AIB)
	}
	req.Body(eltradeString.Val)
	eltrade.Logger.Debugf("fn:Cmd:CreateBill -- C0h request: %s", eltradeString.Val)
	r := dev.Send(req)
	res, err := r.GetData()
	if err != nil {
		eltrade.Logger.Errorf("fn:Cmd:CreateBill -- C0h error: %v", err)
		return "", err
	}
	if strings.Contains(res, "E:") {
		eltrade.Logger.Errorf("fn:Cmd:CreateBill -- C0h failed: %s", res)
		return "", fmt.Errorf("device initialization failed: %s", res)
	}

	req = eltrade.NewRequest(eltrade.ADD_BILL_ITEM)
	for _, product := range bill.Products {
		eltradeString = eltrade.EltradeString{}
		eltradeString.AppendWD(product.Label, "")
		if strings.TrimSpace(product.BarCode) != "" {
			eltradeString.Append(fmt.Sprintf("\n%s", product.BarCode))
		}
		eltradeString.AppendWD("", "\t")
		eltradeString.AppendWD(product.Tax, "")
		eltradeString.AppendWD(fmt.Sprintf("%f", product.Price), "")
		eltradeString.AppendWD(fmt.Sprintf("%f", product.Items), "*")
		if strings.TrimSpace(product.SpecificTax) != "" {
			eltradeString.AppendWD(product.SpecificTax, ";")
			eltradeString.Append(product.SpecificTaxDesc)
		}
		if strings.TrimSpace(product.OriginalPrice) != "" {
			eltradeString.AppendWD(product.OriginalPrice, "\t")
			eltradeString.AppendWD(product.PriceChangeExplanation, ",")
		}
		req.Body(eltradeString.Val)
		eltrade.Logger.Debugf("fn:Cmd:CreateBill -- ADD_BILL_ITEM request: %s", eltradeString.Val)
		r = dev.Send(req)
		res, err = r.GetData()
		if err != nil {
			eltrade.Logger.Errorf("fn:Cmd:CreateBill -- ADD_BILL_ITEM error: %v", err)
			return "", err
		}
	}

	req = eltrade.NewRequest(eltrade.GET_BILL_TOTAL)
	for _, payment := range bill.Payments {
		count := 1
		for {
			eltradeString = eltrade.EltradeString{Val: fmt.Sprintf("%s%f", payment.Mode, payment.Amount)}
			req.Body(eltradeString.Val)
			eltrade.Logger.Debugf("fn:Cmd:CreateBill -- GET_BILL_TOTAL request: %s", eltradeString.Val)
			r = dev.Send(req)
			res, err = r.GetData()
			if err != nil {
				eltrade.Logger.Errorf("fn:Cmd:CreateBill -- GET_BILL_TOTAL error: %v", err)
				return "", err
			}
			if count == 3 || res[0] == 'R' {
				break
			}
			count++
		}
	}

	req = eltrade.NewRequest(eltrade.END_BILL)
	req.Body(eltradeString.Val)
	eltrade.Logger.Debugf("fn:Cmd:CreateBill -- END_BILL request: %s", eltradeString.Val)
	r = dev.Send(req)
	res, err = r.GetData()
	if err != nil {
		eltrade.Logger.Errorf("fn:Cmd:CreateBill -- END_BILL error: %v", err)
		return "", err
	}
	splitedRes := strings.Split(res, ",")
	if len(splitedRes) < 7 {
		eltrade.Logger.Errorf("fn:Cmd:CreateBill -- Invalid END_BILL response: %s", res)
		return res, fmt.Errorf("invalid cmd response: %s", res)
	}
	eltrade.Logger.Debugf("fn:Cmd:CreateBill -- END_BILL response: %v", splitedRes)
	return fmt.Sprintf("F;%s;%s;%s;%s", splitedRes[4], splitedRes[6], splitedRes[5], splitedRes[3]), nil
}
