package cloudfoundry

import (
	"context"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/terraform-providers/terraform-provider-cloudfoundry/cloudfoundry/managers"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func resourcePrivateDomainAccess() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourcePrivateDomainAccessCreate,
		ReadContext:   resourcePrivateDomainAccessRead,
		DeleteContext: resourcePrivateDomainAccessDelete,
		Importer: &schema.ResourceImporter{
			StateContext: ImportReadContext(resourcePrivateDomainAccessRead),
		},

		Schema: map[string]*schema.Schema{
			"domain": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"org": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
		},
	}
}

func resourcePrivateDomainAccessCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	session := meta.(*managers.Session)
	domain := d.Get("domain").(string)
	org := d.Get("org").(string)
	_, err := session.ClientV2.SetOrganizationPrivateDomain(domain, org)
	if err != nil {
		return diag.FromErr(err)
	}

	d.SetId(computeID(org, domain))
	return nil
}

func resourcePrivateDomainAccessRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	session := meta.(*managers.Session)

	id := d.Id()
	// id in read hook comes from create or import callback which ensure id's validity
	orgGuid, domainGuid, _ := parseID(id)

	found := false
	domains, _, err := session.ClientV2.GetOrganizationPrivateDomains(orgGuid)
	if err != nil {
		return diag.FromErr(err)
	}
	for _, domain := range domains {
		if domain.GUID == domainGuid {
			found = true
			break
		}
	}
	if !found {
		d.SetId("")
		return nil
	}

	d.Set("org", orgGuid)
	d.Set("domain", domainGuid)
	return nil
}

func resourcePrivateDomainAccessDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	session := meta.(*managers.Session)

	id := d.Id()

	org, domain, _ := parseID(id)
	_, err := session.ClientV2.DeleteOrganizationPrivateDomain(org, domain)
	return diag.FromErr(err)
}
