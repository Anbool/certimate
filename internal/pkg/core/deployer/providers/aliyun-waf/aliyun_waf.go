﻿package aliyunwaf

import (
	"context"
	"errors"
	"fmt"
	"strings"

	aliyunOpen "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	"github.com/alibabacloud-go/tea/tea"
	aliyunWaf "github.com/alibabacloud-go/waf-openapi-20211001/v5/client"
	xerrors "github.com/pkg/errors"

	"github.com/usual2970/certimate/internal/pkg/core/deployer"
	"github.com/usual2970/certimate/internal/pkg/core/logger"
	"github.com/usual2970/certimate/internal/pkg/core/uploader"
	uploadersp "github.com/usual2970/certimate/internal/pkg/core/uploader/providers/aliyun-cas"
)

type DeployerConfig struct {
	// 阿里云 AccessKeyId。
	AccessKeyId string `json:"accessKeyId"`
	// 阿里云 AccessKeySecret。
	AccessKeySecret string `json:"accessKeySecret"`
	// 阿里云地域。
	Region string `json:"region"`
	// WAF 实例 ID。
	InstanceId string `json:"instanceId"`
	// 接入域名（支持泛域名）。
	Domain string `json:"domain,omitempty"`
}

type DeployerProvider struct {
	config      *DeployerConfig
	logger      logger.Logger
	sdkClient   *aliyunWaf.Client
	sslUploader uploader.Uploader
}

var _ deployer.Deployer = (*DeployerProvider)(nil)

func NewDeployer(config *DeployerConfig) (*DeployerProvider, error) {
	if config == nil {
		panic("config is nil")
	}

	client, err := createSdkClient(config.AccessKeyId, config.AccessKeySecret, config.Region)
	if err != nil {
		return nil, xerrors.Wrap(err, "failed to create sdk client")
	}

	uploader, err := createSslUploader(config.AccessKeyId, config.AccessKeySecret, config.Region)
	if err != nil {
		return nil, xerrors.Wrap(err, "failed to create ssl uploader")
	}

	return &DeployerProvider{
		config:      config,
		logger:      logger.NewNilLogger(),
		sdkClient:   client,
		sslUploader: uploader,
	}, nil
}

func (d *DeployerProvider) WithLogger(logger logger.Logger) *DeployerProvider {
	d.logger = logger
	return d
}

func (d *DeployerProvider) Deploy(ctx context.Context, certPem string, privkeyPem string) (*deployer.DeployResult, error) {
	if d.config.InstanceId == "" {
		return nil, errors.New("config `instanceId` is required")
	}

	// 上传证书到 CAS
	upres, err := d.sslUploader.Upload(ctx, certPem, privkeyPem)
	if err != nil {
		return nil, xerrors.Wrap(err, "failed to upload certificate file")
	} else {
		d.logger.Logt("certificate file uploaded", upres)
	}

	if d.config.Domain == "" {
		// 未指定接入域名，只需替换默认证书即可

		// 查询默认 SSL/TLS 设置
		// REF: https://help.aliyun.com/zh/waf/web-application-firewall-3-0/developer-reference/api-waf-openapi-2021-10-01-describedefaulthttps
		describeDefaultHttpsReq := &aliyunWaf.DescribeDefaultHttpsRequest{
			InstanceId: tea.String(d.config.InstanceId),
			RegionId:   tea.String(d.config.Region),
		}
		describeDefaultHttpsResp, err := d.sdkClient.DescribeDefaultHttps(describeDefaultHttpsReq)
		if err != nil {
			return nil, xerrors.Wrap(err, "failed to execute sdk request 'waf.DescribeDefaultHttps'")
		} else {
			d.logger.Logt("已查询到默认 SSL/TLS 设置", describeDefaultHttpsResp)
		}

		// 修改默认 SSL/TLS 设置
		// REF: https://help.aliyun.com/zh/waf/web-application-firewall-3-0/developer-reference/api-waf-openapi-2021-10-01-modifydefaulthttps
		modifyDefaultHttpsReq := &aliyunWaf.ModifyDefaultHttpsRequest{
			InstanceId:  tea.String(d.config.InstanceId),
			RegionId:    tea.String(d.config.Region),
			CertId:      tea.String(upres.CertId),
			TLSVersion:  tea.String("tlsv1"),
			EnableTLSv3: tea.Bool(false),
		}
		if describeDefaultHttpsResp.Body != nil && describeDefaultHttpsResp.Body.DefaultHttps != nil {
			modifyDefaultHttpsReq.TLSVersion = describeDefaultHttpsResp.Body.DefaultHttps.TLSVersion
			modifyDefaultHttpsReq.EnableTLSv3 = describeDefaultHttpsResp.Body.DefaultHttps.EnableTLSv3
		}
		modifyDefaultHttpsResp, err := d.sdkClient.ModifyDefaultHttps(modifyDefaultHttpsReq)
		if err != nil {
			return nil, xerrors.Wrap(err, "failed to execute sdk request 'waf.ModifyDefaultHttps'")
		} else {
			d.logger.Logt("已修改默认 SSL/TLS 设置", modifyDefaultHttpsResp)
		}
	} else {
		// 指定接入域名

		// 查询 CNAME 接入详情
		// REF: https://help.aliyun.com/zh/waf/web-application-firewall-3-0/developer-reference/api-waf-openapi-2021-10-01-describedomaindetail
		describeDomainDetailReq := &aliyunWaf.DescribeDomainDetailRequest{
			InstanceId: tea.String(d.config.InstanceId),
			RegionId:   tea.String(d.config.Region),
			Domain:     tea.String(d.config.Domain),
		}
		describeDomainDetailResp, err := d.sdkClient.DescribeDomainDetail(describeDomainDetailReq)
		if err != nil {
			return nil, xerrors.Wrap(err, "failed to execute sdk request 'waf.DescribeDomainDetail'")
		} else {
			d.logger.Logt("已查询到 CNAME 接入详情", describeDomainDetailResp)
		}

		// 修改 CNAME 接入资源
		// REF: https://help.aliyun.com/zh/waf/web-application-firewall-3-0/developer-reference/api-waf-openapi-2021-10-01-modifydomain
		modifyDomainReq := &aliyunWaf.ModifyDomainRequest{
			InstanceId: tea.String(d.config.InstanceId),
			RegionId:   tea.String(d.config.Region),
			Domain:     tea.String(d.config.Domain),
			Listen: &aliyunWaf.ModifyDomainRequestListen{
				CertId:      tea.String(upres.CertId),
				TLSVersion:  tea.String("tlsv1"),
				EnableTLSv3: tea.Bool(false),
			},
			Redirect: &aliyunWaf.ModifyDomainRequestRedirect{},
		}
		if describeDomainDetailResp.Body != nil && describeDomainDetailResp.Body.Listen != nil {
			modifyDomainReq.Listen.TLSVersion = describeDomainDetailResp.Body.Listen.TLSVersion
			modifyDomainReq.Listen.EnableTLSv3 = describeDomainDetailResp.Body.Listen.EnableTLSv3
			modifyDomainReq.Listen.FocusHttps = describeDomainDetailResp.Body.Listen.FocusHttps
		}
		modifyDomainResp, err := d.sdkClient.ModifyDomain(modifyDomainReq)
		if err != nil {
			return nil, xerrors.Wrap(err, "failed to execute sdk request 'waf.ModifyDomain'")
		} else {
			d.logger.Logt("已修改 CNAME 接入资源", modifyDomainResp)
		}
	}

	return &deployer.DeployResult{}, nil
}

func createSdkClient(accessKeyId, accessKeySecret, region string) (*aliyunWaf.Client, error) {
	// 接入点一览：https://api.aliyun.com/product/waf-openapi
	config := &aliyunOpen.Config{
		AccessKeyId:     tea.String(accessKeyId),
		AccessKeySecret: tea.String(accessKeySecret),
		Endpoint:        tea.String(fmt.Sprintf("wafopenapi.%s.aliyuncs.com", region)),
	}

	client, err := aliyunWaf.NewClient(config)
	if err != nil {
		return nil, err
	}

	return client, nil
}

func createSslUploader(accessKeyId, accessKeySecret, region string) (uploader.Uploader, error) {
	casRegion := region
	if casRegion != "" {
		// 阿里云 CAS 服务接入点是独立于 WAF 服务的
		// 国内版固定接入点：华东一杭州
		// 国际版固定接入点：亚太东南一新加坡
		if casRegion != "" && !strings.HasPrefix(casRegion, "cn-") {
			casRegion = "ap-southeast-1"
		} else {
			casRegion = "cn-hangzhou"
		}
	}

	uploader, err := uploadersp.NewUploader(&uploadersp.UploaderConfig{
		AccessKeyId:     accessKeyId,
		AccessKeySecret: accessKeySecret,
		Region:          casRegion,
	})
	return uploader, err
}
