package cloudfoundry

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/customdiff"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"

	"code.cloudfoundry.org/cli/api/cloudcontroller/ccv2"
	"code.cloudfoundry.org/cli/api/cloudcontroller/ccv2/constant"
	"github.com/terraform-providers/terraform-provider-cloudfoundry/cloudfoundry/managers"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func resourceServiceInstance() *schema.Resource {

	return &schema.Resource{

		CreateContext: resourceServiceInstanceCreate,
		ReadContext:   resourceServiceInstanceRead,
		UpdateContext: resourceServiceInstanceUpdate,
		DeleteContext: resourceServiceInstanceDelete,

		SchemaVersion: 1,

		MigrateState: resourceServiceInstanceMigrateState,

		Importer: &schema.ResourceImporter{
			StateContext: resourceServiceInstanceImport,
		},

		Timeouts: &schema.ResourceTimeout{
			Create: schema.DefaultTimeout(15 * time.Minute),
			Update: schema.DefaultTimeout(15 * time.Minute),
			Delete: schema.DefaultTimeout(15 * time.Minute),
		},

		Schema: map[string]*schema.Schema{
			"name": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
			},
			"service_plan": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
			},
			"space": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"json_params": &schema.Schema{
				Type:         schema.TypeString,
				Optional:     true,
				Default:      "",
				ValidateFunc: validation.StringIsJSON,
			},
			"replace_on_service_plan_change": {
				Type:     schema.TypeBool,
				Optional: true,
				Default:  false,
			},
			"replace_on_params_change": {
				Type:     schema.TypeBool,
				Optional: true,
				Default:  false,
			},
			"tags": &schema.Schema{
				Type:     schema.TypeList,
				Optional: true,
				Elem:     &schema.Schema{Type: schema.TypeString},
			},
			"recursive_delete": &schema.Schema{
				Type:     schema.TypeBool,
				Optional: true,
				Default:  false,
			},
		},
		CustomizeDiff: customdiff.All(
			customdiff.ForceNewIf(
				"service_plan", func(_ context.Context, d *schema.ResourceDiff, meta interface{}) bool {
					if ok := d.Get("replace_on_service_plan_change").(bool); ok {
						return true
					}
					return false
				}),
			customdiff.ForceNewIf(
				"params", func(_ context.Context, d *schema.ResourceDiff, meta interface{}) bool {
					if ok := d.Get("replace_on_params_change").(bool); ok {
						return true
					}
					return false
				},
			),
		),
	}
}

func resourceServiceInstanceCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	session := meta.(*managers.Session)

	name := d.Get("name").(string)
	servicePlan := d.Get("service_plan").(string)
	space := d.Get("space").(string)
	jsonParameters := d.Get("json_params").(string)
	tags := make([]string, 0)
	for _, v := range d.Get("tags").([]interface{}) {
		tags = append(tags, v.(string))
	}

	params := make(map[string]interface{})
	if len(jsonParameters) > 0 {
		err := json.Unmarshal([]byte(jsonParameters), &params)
		if err != nil {
			return diag.FromErr(err)
		}
	}
	si, _, err := session.ClientV2.CreateServiceInstance(space, servicePlan, name, params, tags)
	if err != nil {
		return diag.FromErr(err)
	}
	stateConf := &resource.StateChangeConf{
		Pending:        resourceServiceInstancePendingStates,
		Target:         resourceServiceInstanceSuccessStates,
		Refresh:        resourceServiceInstanceStateFunc(si.GUID, "create", meta),
		Timeout:        d.Timeout(schema.TimeoutCreate),
		PollInterval:   30 * time.Second,
		Delay:          5 * time.Second,
		NotFoundChecks: 6, // if the CF object for the instance isn't at least present after 3 minutes, it's probably not coming
	}

	// Wait, catching any errors
	if _, err = stateConf.WaitForStateContext(ctx); err != nil {
		return diag.FromErr(err)
	}

	d.SetId(si.GUID)

	return nil
}

func resourceServiceInstanceRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	session := meta.(*managers.Session)

	serviceInstance, _, err := session.ClientV2.GetServiceInstance(d.Id())
	if err != nil {
		if IsErrNotFound(err) {
			d.SetId("")
			return nil
		}
		return diag.FromErr(err)
	}

	d.Set("name", serviceInstance.Name)
	d.Set("service_plan", serviceInstance.ServicePlanGUID)
	d.Set("space", serviceInstance.SpaceGUID)

	if serviceInstance.Tags != nil {
		tags := make([]interface{}, len(serviceInstance.Tags))
		for i, v := range serviceInstance.Tags {
			tags[i] = v
		}
		d.Set("tags", tags)
	} else {
		d.Set("tags", nil)
	}

	return nil
}

func resourceServiceInstanceUpdate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	session := meta.(*managers.Session)
	if session == nil {
		return diag.Errorf("client is nil")
	}

	// Enable partial state mode
	// We need to explicitly set state updates ourselves or
	// tell terraform when a state change is applied and thus okay to persist
	// In particular this is necessary for params since we cannot query CF for
	// the current value of this field
	d.Partial(true)

	var (
		id, name string
		tags     []string
		params   map[string]interface{}
	)

	id = d.Id()
	name = d.Get("name").(string)
	servicePlan := d.Get("service_plan").(string)
	jsonParameters := d.Get("json_params").(string)

	if len(jsonParameters) > 0 {
		err := json.Unmarshal([]byte(jsonParameters), &params)
		if err != nil {
			return diag.FromErr(err)
		}
	}

	for _, v := range d.Get("tags").([]interface{}) {
		tags = append(tags, v.(string))
	}

	_, _, err := session.ClientV2.UpdateServiceInstance(ccv2.ServiceInstance{
		GUID:            id,
		Name:            name,
		ServicePlanGUID: servicePlan,
		Parameters:      params,
		Tags:            tags,
	})
	if err != nil {
		return diag.FromErr(err)
	}

	stateConf := &resource.StateChangeConf{
		Pending:        resourceServiceInstancePendingStates,
		Target:         resourceServiceInstanceSuccessStates,
		Refresh:        resourceServiceInstanceStateFunc(id, "update", meta),
		Timeout:        d.Timeout(schema.TimeoutUpdate),
		PollInterval:   30 * time.Second,
		Delay:          5 * time.Second,
		NotFoundChecks: 3, // if we don't find the service instance in CF during an update, something is definitely wrong
	}
	// Wait, catching any errors
	if _, err = stateConf.WaitForStateContext(ctx); err != nil {
		return diag.FromErr(err)
	}

	// We succeeded, disable partial mode
	d.Partial(false)
	return nil
}

func resourceServiceInstanceDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	session := meta.(*managers.Session)
	id := d.Id()

	recursiveDelete := d.Get("recursive_delete").(bool)
	async, _, err := session.ClientV2.DeleteServiceInstance(id, recursiveDelete, session.PurgeWhenDelete)
	if err != nil {
		return diag.FromErr(err)
	}
	if !async {
		return nil
	}
	stateConf := &resource.StateChangeConf{
		Pending:      resourceServiceInstancePendingStates,
		Target:       []string{},
		Refresh:      resourceServiceInstanceStateFunc(id, "delete", meta),
		Timeout:      d.Timeout(schema.TimeoutDelete),
		PollInterval: 30 * time.Second,
		Delay:        5 * time.Second,
	}
	// Wait, catching any errors
	if _, err = stateConf.WaitForStateContext(ctx); err != nil {
		return diag.FromErr(err)
	}

	return nil
}

func resourceServiceInstanceImport(ctx context.Context, d *schema.ResourceData, meta interface{}) ([]*schema.ResourceData, error) {
	session := meta.(*managers.Session)

	serviceinstance, _, err := session.ClientV2.GetServiceInstance(d.Id())

	if err != nil {
		return nil, err
	}

	d.Set("name", serviceinstance.Name)
	d.Set("service_plan", serviceinstance.ServicePlanGUID)
	d.Set("space", serviceinstance.SpaceGUID)
	d.Set("tags", serviceinstance.Tags)

	d.Set("replace_on_service_plan_change", false)
	d.Set("replace_on_params_change", false)

	return ImportReadContext(resourceServiceInstanceRead)(ctx, d, meta)
}

func resourceServiceInstanceStateFunc(serviceInstanceID string, operationType string, meta interface{}) resource.StateRefreshFunc {
	return func() (interface{}, string, error) {
		session := meta.(*managers.Session)
		serviceInstance, _, err := session.ClientV2.GetServiceInstance(serviceInstanceID)
		if err != nil {
			// We should get a 404 if the resource doesn't exist (eg. it has been deleted)
			// In this case, the refresh code is expecting a nil object
			if IsErrNotFound(err) {
				return nil, "", nil
			}
			return nil, "", err
		}

		if serviceInstance.LastOperation.Type == operationType {
			stateStr := string(serviceInstance.LastOperation.State)
			switch serviceInstance.LastOperation.State {
			case constant.LastOperationSucceeded:
				return serviceInstance, stateStr, nil
			case constant.LastOperationFailed:
				return nil, stateStr, fmt.Errorf("%s", serviceInstance.LastOperation.Description)
			}
			return serviceInstance, stateStr, nil
		}

		return serviceInstance, "wrong operation", nil
	}
}

var resourceServiceInstancePendingStates = []string{
	"in progress",
}

var resourceServiceInstanceSuccessStates = []string{
	"succeeded",
}
