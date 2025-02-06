package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/usual2970/certimate/internal/app"
	"github.com/usual2970/certimate/internal/domain"
)

type WorkflowOutputRepository struct{}

func NewWorkflowOutputRepository() *WorkflowOutputRepository {
	return &WorkflowOutputRepository{}
}

func (r *WorkflowOutputRepository) GetByNodeId(ctx context.Context, workflowNodeId string) (*domain.WorkflowOutput, error) {
	records, err := app.GetApp().FindRecordsByFilter(
		domain.CollectionNameWorkflowOutput,
		"nodeId={:nodeId}",
		"-created",
		1, 0,
		dbx.Params{"nodeId": workflowNodeId},
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrRecordNotFound
		}
		return nil, err
	}
	if len(records) == 0 {
		return nil, domain.ErrRecordNotFound
	}

	return r.castRecordToModel(records[0])
}

func (r *WorkflowOutputRepository) Save(ctx context.Context, workflowOutput *domain.WorkflowOutput) (*domain.WorkflowOutput, error) {
	record, err := r.saveRecord(workflowOutput)
	if err != nil {
		return workflowOutput, err
	}

	workflowOutput.Id = record.Id
	workflowOutput.CreatedAt = record.GetDateTime("created").Time()
	workflowOutput.UpdatedAt = record.GetDateTime("updated").Time()
	return workflowOutput, nil
}

func (r *WorkflowOutputRepository) SaveWithCertificate(ctx context.Context, workflowOutput *domain.WorkflowOutput, certificate *domain.Certificate) (*domain.WorkflowOutput, error) {
	record, err := r.saveRecord(workflowOutput)
	if err != nil {
		return workflowOutput, err
	} else {
		workflowOutput.Id = record.Id
		workflowOutput.CreatedAt = record.GetDateTime("created").Time()
		workflowOutput.UpdatedAt = record.GetDateTime("updated").Time()
	}

	if certificate != nil {
		certificate.WorkflowId = workflowOutput.WorkflowId
		certificate.WorkflowRunId = workflowOutput.RunId
		certificate.WorkflowNodeId = workflowOutput.NodeId
		certificate.WorkflowOutputId = workflowOutput.Id
		certificate, err := NewCertificateRepository().Save(ctx, certificate)
		if err != nil {
			return workflowOutput, err
		}

		// 写入证书 ID 到工作流输出结果中
		for i, item := range workflowOutput.Outputs {
			if item.Name == string(domain.WorkflowNodeIONameCertificate) {
				workflowOutput.Outputs[i].Value = certificate.Id
				break
			}
		}
		record.Set("outputs", workflowOutput.Outputs)
		if err := app.GetApp().Save(record); err != nil {
			return workflowOutput, err
		}
	}

	return workflowOutput, err
}

func (r *WorkflowOutputRepository) castRecordToModel(record *core.Record) (*domain.WorkflowOutput, error) {
	if record == nil {
		return nil, fmt.Errorf("record is nil")
	}

	node := &domain.WorkflowNode{}
	if err := record.UnmarshalJSONField("node", node); err != nil {
		return nil, errors.New("failed to unmarshal node")
	}

	outputs := make([]domain.WorkflowNodeIO, 0)
	if err := record.UnmarshalJSONField("outputs", &outputs); err != nil {
		return nil, errors.New("failed to unmarshal output")
	}

	workflowOutput := &domain.WorkflowOutput{
		Meta: domain.Meta{
			Id:        record.Id,
			CreatedAt: record.GetDateTime("created").Time(),
			UpdatedAt: record.GetDateTime("updated").Time(),
		},
		WorkflowId: record.GetString("workflowId"),
		RunId:      record.GetString("runId"),
		NodeId:     record.GetString("nodeId"),
		Node:       node,
		Outputs:    outputs,
		Succeeded:  record.GetBool("succeeded"),
	}
	return workflowOutput, nil
}

func (r *WorkflowOutputRepository) saveRecord(output *domain.WorkflowOutput) (*core.Record, error) {
	collection, err := app.GetApp().FindCollectionByNameOrId(domain.CollectionNameWorkflowOutput)
	if err != nil {
		return nil, err
	}

	var record *core.Record
	if output.Id == "" {
		record = core.NewRecord(collection)
	} else {
		record, err = app.GetApp().FindRecordById(collection, output.Id)
		if err != nil {
			return record, err
		}
	}
	record.Set("workflowId", output.WorkflowId)
	record.Set("runId", output.RunId)
	record.Set("nodeId", output.NodeId)
	record.Set("node", output.Node)
	record.Set("outputs", output.Outputs)
	record.Set("succeeded", output.Succeeded)
	if err := app.GetApp().Save(record); err != nil {
		return record, err
	}

	return record, err
}
